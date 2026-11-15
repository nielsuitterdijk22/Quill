// Package egress is the pipeline egress control (2026-11-15, replacing
// Forge's squid gateway). A policy is either "allow all" or an allowlist of
// hostnames; the api resolves it per run (organization policy, inherited by
// the repository, plus the repository's own extra hosts) into the JobSpec,
// and the CI worker enforces it on the instance: a transparent proxy that
// every outbound HTTP/HTTPS connection is redirected through checks the
// hostname (TLS SNI / HTTP Host) against the list, and everything that is
// not HTTP(S) or DNS is rejected.
package egress

import (
	"fmt"
	"net"
	"net/url"
	"sort"
	"strings"
)

// Modes of a policy.
const (
	ModeAllowlist = "allowlist"
	ModeAllowAll  = "allow_all"
)

// MaxHosts bounds one scope's list (organization or repository).
const MaxHosts = 300

// Policy is what a run carries to the worker.
type Policy struct {
	Mode  string   `json:"mode"`
	Hosts []string `json:"hosts,omitempty"`
}

// Enforced reports whether the policy restricts traffic.
func (p *Policy) Enforced() bool { return p != nil && p.Mode == ModeAllowlist }

// Group is a labelled set of default hosts (the settings page shows them
// grouped).
type Group struct {
	Name  string   `json:"name"`
	Hosts []string `json:"hosts"`
}

// DefaultGroups are the public registries and toolchain hosts every
// organization starts with. "*.x" matches subdomains of x, not x itself.
var DefaultGroups = []Group{
	{"GitHub", []string{"github.com", "*.github.com", "*.githubusercontent.com", "ghcr.io"}},
	{"Docker Hub", []string{"docker.io", "*.docker.io", "*.docker.com"}},
	{"Other container registries", []string{"quay.io", "*.quay.io", "gcr.io", "registry.k8s.io", "mcr.microsoft.com", "*.data.mcr.microsoft.com", "public.ecr.aws"}},
	{"npm / Node.js", []string{"registry.npmjs.org", "registry.yarnpkg.com", "repo.yarnpkg.com", "nodejs.org", "fonts.googleapis.com", "fonts.gstatic.com"}},
	{"Python", []string{"pypi.org", "files.pythonhosted.org", "*.python.org"}},
	{"Maven / Gradle", []string{"repo.maven.apache.org", "repo1.maven.org", "plugins.gradle.org", "services.gradle.org", "downloads.gradle.org"}},
	// proxy.golang.org serves module zips by redirecting to storage.googleapis.com.
	{"Go", []string{"proxy.golang.org", "sum.golang.org", "go.dev", "dl.google.com", "storage.googleapis.com"}},
	{"Rust", []string{"crates.io", "*.crates.io", "static.rust-lang.org", "sh.rustup.rs"}},
	{"Ruby / PHP / .NET", []string{"rubygems.org", "*.rubygems.org", "packagist.org", "repo.packagist.org", "api.nuget.org", "*.nuget.org", "dot.net", "builds.dotnet.microsoft.com"}},
	{"OS packages", []string{"deb.debian.org", "security.debian.org", "archive.ubuntu.com", "security.ubuntu.com", "ports.ubuntu.com", "dl-cdn.alpinelinux.org"}},
	{"Terraform", []string{"registry.terraform.io", "releases.hashicorp.com", "checkpoint-api.hashicorp.com"}},
}

// DefaultHosts is DefaultGroups flattened.
func DefaultHosts() []string {
	var out []string
	for _, g := range DefaultGroups {
		out = append(out, g.Hosts...)
	}
	return out
}

// RequiredHosts are always allowed, whatever the policy says: the platform
// itself (checkout, logs, the job's own api calls), github.com for `uses:`
// actions (they resolve there first), and the registries of the runner
// images the worker pulls.
func RequiredHosts(apiURL string, images ...string) []string {
	out := []string{"github.com", "api.github.com", "codeload.github.com", "*.githubusercontent.com"}
	if u, err := url.Parse(apiURL); err == nil && u.Hostname() != "" {
		out = append(out, strings.ToLower(u.Hostname()))
	}
	for _, img := range images {
		out = append(out, registryHosts(img)...)
	}
	return Normalize(out)
}

// registryHosts returns the hosts an image pull from ref reaches.
func registryHosts(ref string) []string {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil
	}
	first, _, found := strings.Cut(ref, "/")
	if found && (strings.ContainsAny(first, ".:") || first == "localhost") {
		h := first
		if host, _, err := net.SplitHostPort(first); err == nil {
			h = host
		}
		return []string{strings.ToLower(h)}
	}
	// Docker Hub: registry, token service and the blob CDN.
	return []string{"registry-1.docker.io", "auth.docker.io", "production.cloudflare.docker.com"}
}

// NormalizeHost validates and canonicalises one allowlist entry: a hostname
// (lowercase, no trailing dot) optionally prefixed with "*." for subdomains.
// URLs are reduced to their host; IP addresses and ports are rejected (the
// proxy matches hostnames only).
func NormalizeHost(raw string) (string, error) {
	h := strings.ToLower(strings.TrimSpace(raw))
	if i := strings.Index(h, "://"); i >= 0 {
		u, err := url.Parse(h)
		if err != nil || u.Host == "" {
			return "", fmt.Errorf("%q is not a hostname", raw)
		}
		h = u.Host
	}
	h = strings.TrimSuffix(strings.TrimSuffix(h, "/"), ".")
	if strings.HasPrefix(h, ".") { // Forge/squid style ".example.com"
		h = "*" + h
	}
	wild := strings.HasPrefix(h, "*.")
	name := strings.TrimPrefix(h, "*.")
	if strings.Contains(name, ":") {
		return "", fmt.Errorf("%q: ports are not supported (HTTP and HTTPS only)", raw)
	}
	if net.ParseIP(name) != nil {
		return "", fmt.Errorf("%q: IP addresses are not supported, use a hostname", raw)
	}
	if len(name) > 253 || !strings.Contains(name, ".") {
		return "", fmt.Errorf("%q is not a fully qualified hostname", raw)
	}
	for _, label := range strings.Split(name, ".") {
		if !validLabel(label) {
			return "", fmt.Errorf("%q is not a valid hostname", raw)
		}
	}
	if wild {
		return "*." + name, nil
	}
	return name, nil
}

func validLabel(l string) bool {
	if l == "" || len(l) > 63 || l[0] == '-' || l[len(l)-1] == '-' {
		return false
	}
	for _, c := range l {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

// ParseHosts normalises a user-supplied list: entries are validated,
// deduplicated and sorted; blank entries and "#" comments are skipped.
func ParseHosts(raw []string) ([]string, error) {
	var out []string
	for _, r := range raw {
		if r = strings.TrimSpace(r); r == "" || strings.HasPrefix(r, "#") {
			continue
		}
		h, err := NormalizeHost(r)
		if err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	out = Normalize(out)
	if len(out) > MaxHosts {
		return nil, fmt.Errorf("at most %d hosts", MaxHosts)
	}
	return out, nil
}

// Normalize deduplicates and sorts hosts (already-valid entries).
func Normalize(hosts []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, h := range hosts {
		h = strings.ToLower(strings.TrimSpace(h))
		if h == "" || seen[h] {
			continue
		}
		seen[h] = true
		out = append(out, h)
	}
	sort.Strings(out)
	return out
}

// Matcher answers whether a hostname is allowed.
type Matcher struct {
	exact  map[string]bool
	suffix []string // ".example.com"
}

// NewMatcher compiles a host list.
func NewMatcher(hosts []string) *Matcher {
	m := &Matcher{exact: map[string]bool{}}
	for _, h := range hosts {
		h = strings.ToLower(strings.TrimSpace(h))
		if strings.HasPrefix(h, "*.") {
			m.suffix = append(m.suffix, h[1:])
		} else if h != "" {
			m.exact[h] = true
		}
	}
	return m
}

// Allowed reports whether host matches the list.
func (m *Matcher) Allowed(host string) bool {
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	if host == "" {
		return false
	}
	if m.exact[host] {
		return true
	}
	for _, s := range m.suffix {
		if strings.HasSuffix(host, s) && len(host) > len(s) {
			return true
		}
	}
	return false
}
