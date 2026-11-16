package egress

import (
	"bufio"
	"context"
	"crypto/tls"
	"io"
	"net"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestNormalizeHost(t *testing.T) {
	ok := map[string]string{
		"Registry.NPMJS.org":          "registry.npmjs.org",
		"  pypi.org. ":                "pypi.org",
		"*.Docker.io":                 "*.docker.io",
		".githubusercontent.com":      "*.githubusercontent.com",
		"https://repo.maven.org/x/y/": "repo.maven.org",
	}
	for in, want := range ok {
		if got, err := NormalizeHost(in); err != nil || got != want {
			t.Errorf("NormalizeHost(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"localhost", "10.0.0.1", "example.com:8443", "*", "*.com.", "exa mple.com", "-x.com", "a..b", "::1"} {
		if got, err := NormalizeHost(bad); err == nil {
			t.Errorf("NormalizeHost(%q) = %q, want error", bad, got)
		}
	}
	got, err := ParseHosts([]string{"b.com", "# comment", "", "A.com", "a.com"})
	if err != nil || !slices.Equal(got, []string{"a.com", "b.com"}) {
		t.Fatalf("ParseHosts = %v, %v", got, err)
	}
	many := make([]string, MaxHosts+1)
	for i := range many {
		many[i] = "h" + strings.Repeat("x", i%50) + string(rune('a'+i%26)) + ".example.com"
	}
	if _, err := ParseHosts(many); err == nil && len(Normalize(many)) > MaxHosts {
		t.Fatal("over MaxHosts accepted")
	}
}

func TestDefaultsAreValid(t *testing.T) {
	for _, h := range DefaultHosts() {
		if n, err := NormalizeHost(h); err != nil || n != h {
			t.Errorf("default %q: %q %v", h, n, err)
		}
	}
	m := NewMatcher(DefaultHosts())
	for _, h := range []string{"registry-1.docker.io", "registry.npmjs.org", "files.pythonhosted.org", "repo1.maven.org", "proxy.golang.org", "storage.googleapis.com", "ghcr.io", "objects.githubusercontent.com"} {
		if !m.Allowed(h) {
			t.Errorf("defaults should allow %s", h)
		}
	}
}

func TestRequiredHosts(t *testing.T) {
	got := RequiredHosts("https://api.black-tulip.nl", "node:22-bookworm", "rg.fr-par.scw.cloud/quill/ci:1")
	for _, want := range []string{"api.black-tulip.nl", "github.com", "registry-1.docker.io", "production.cloudflare.docker.com", "rg.fr-par.scw.cloud"} {
		if !slices.Contains(got, want) {
			t.Errorf("RequiredHosts missing %s: %v", want, got)
		}
	}
}

func TestMatcher(t *testing.T) {
	m := NewMatcher([]string{"example.com", "*.cdn.net"})
	for h, want := range map[string]bool{
		"example.com": true, "EXAMPLE.com.": true, "www.example.com": false,
		"a.cdn.net": true, "a.b.cdn.net": true, "cdn.net": false, "evilcdn.net": false, "": false,
	} {
		if m.Allowed(h) != want {
			t.Errorf("Allowed(%q) = %v", h, !want)
		}
	}
}

// proxyFixture runs a proxy whose upstream dial records the target and
// returns a pipe to a fake upstream echoing what it received.
type proxyFixture struct {
	p       *Proxy
	tlsAddr string
	plain   string
	mu      sync.Mutex
	dialed  []string
	blocked []string
}

func newProxyFixture(t *testing.T) *proxyFixture {
	t.Helper()
	f := &proxyFixture{}
	f.p = NewProxy(func(ctx context.Context, network, addr string) (net.Conn, error) {
		f.mu.Lock()
		f.dialed = append(f.dialed, addr)
		f.mu.Unlock()
		client, server := net.Pipe()
		go func() { // upstream: first line back, then close
			defer server.Close()
			line, _ := bufio.NewReader(server).ReadString('\n')
			io.WriteString(server, "upstream got: "+line)
		}()
		return client, nil
	})
	for _, l := range []struct {
		addr  *string
		serve func(net.Listener) error
	}{{&f.tlsAddr, f.p.ServeTLS}, {&f.plain, f.p.ServeHTTP}} {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { ln.Close() })
		*l.addr = ln.Addr().String()
		go l.serve(ln)
	}
	f.p.SetPolicy(false, []string{"allowed.example", "*.ok.example"}, func(h string, port int) {
		f.mu.Lock()
		f.blocked = append(f.blocked, h)
		f.mu.Unlock()
	})
	return f
}

func (f *proxyFixture) state() ([]string, []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string{}, f.dialed...), append([]string{}, f.blocked...)
}

func TestProxyHTTPHost(t *testing.T) {
	f := newProxyFixture(t)
	get := func(host string) string {
		c, err := net.Dial("tcp", f.plain)
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		c.SetDeadline(time.Now().Add(5 * time.Second))
		io.WriteString(c, "GET /x HTTP/1.1\r\nHost: "+host+"\r\n\r\n")
		b, _ := io.ReadAll(c)
		return string(b)
	}
	if got := get("allowed.example"); !strings.Contains(got, "upstream got: GET /x HTTP/1.1") {
		t.Fatalf("allowed request not forwarded verbatim: %q", got)
	}
	if got := get("evil.example:80"); !strings.Contains(got, "403 Forbidden") || !strings.Contains(got, "evil.example") {
		t.Fatalf("blocked request: %q", got)
	}
	dialed, blocked := f.state()
	if !slices.Equal(dialed, []string{"allowed.example:80"}) || !slices.Equal(blocked, []string{"evil.example"}) {
		t.Fatalf("dialed %v blocked %v", dialed, blocked)
	}
}

func TestProxyTLSSNI(t *testing.T) {
	f := newProxyFixture(t)
	hello := func(sni string) {
		c, err := net.Dial("tcp", f.tlsAddr)
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		c.SetDeadline(time.Now().Add(5 * time.Second))
		// The handshake can't complete against the fake upstream; only the
		// ClientHello's route matters.
		_ = tls.Client(c, &tls.Config{ServerName: sni, InsecureSkipVerify: sni == ""}).Handshake()
	}
	hello("cdn.ok.example")
	hello("evil.example")
	hello("") // IP-literal style: no SNI
	dialed, blocked := f.state()
	if !slices.Equal(dialed, []string{"cdn.ok.example:443"}) {
		t.Fatalf("dialed %v", dialed)
	}
	if !slices.Equal(blocked, []string{"evil.example", "(no hostname: TLS without server name)"}) {
		t.Fatalf("blocked %v", blocked)
	}
	// Allow-all lets anything through.
	f.p.SetPolicy(true, nil, nil)
	hello("evil.example")
	if dialed, _ := f.state(); !slices.Contains(dialed, "evil.example:443") {
		t.Fatalf("allow-all did not forward: %v", dialed)
	}
}

// TestProxyEmptyConnectionsAreNotRefusals: a connection that closes (or goes
// idle) before sending a byte never tried to reach anything — clients racing
// parallel connection attempts drop the losers this way — so it must not
// count toward the refusal limit that stops a run (it stopped every deploy's
// npm ci on 2026-11-16). Bytes without a hostname still count, labelled.
func TestProxyEmptyConnectionsAreNotRefusals(t *testing.T) {
	f := newProxyFixture(t)
	for i := 0; i < 10; i++ {
		c, err := net.Dial("tcp", f.tlsAddr)
		if err != nil {
			t.Fatal(err)
		}
		c.Close()
	}
	c, err := net.Dial("tcp", f.tlsAddr)
	if err != nil {
		t.Fatal(err)
	}
	c.SetDeadline(time.Now().Add(5 * time.Second))
	io.WriteString(c, "SSH-2.0-OpenSSH_9.6\r\n")
	c.(*net.TCPConn).CloseWrite()
	_, _ = io.ReadAll(c)
	c.Close()
	deadline := time.Now().Add(3 * time.Second)
	for {
		_, blocked := f.state()
		if len(blocked) > 0 || time.Now().After(deadline) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	time.Sleep(100 * time.Millisecond) // let any stray empty-connection handler finish
	if _, blocked := f.state(); !slices.Equal(blocked, []string{"(no hostname: not TLS, 21 bytes)"}) {
		t.Fatalf("blocked %v, want only the non-TLS connection", blocked)
	}
}

func TestPublicDialerRefusesPrivate(t *testing.T) {
	dial := PublicDialer(nil)
	srv, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	if c, err := dial(context.Background(), "tcp", srv.Addr().String()); err == nil {
		c.Close()
		t.Fatal("dialed loopback")
	}
	for ip, want := range map[string]bool{"169.254.42.42": false, "10.1.2.3": false, "100.64.0.1": false, "8.8.8.8": true, "2001:4860::8888": true, "fd00:42::42": false} {
		if PublicIP(net.ParseIP(ip)) != want {
			t.Errorf("PublicIP(%s) != %v", ip, want)
		}
	}
}

func TestFirewallRules(t *testing.T) {
	var cmds []string
	fw := &Firewall{V4: "iptables-nft", V6: "ip6tables-nft", Run: func(bin string, args ...string) error {
		cmd := bin + " " + strings.Join(args, " ")
		cmds = append(cmds, cmd)
		if strings.Contains(cmd, " -C ") {
			return io.EOF // jump not present yet
		}
		return nil
	}}
	if err := fw.Init(); err != nil {
		t.Fatal(err)
	}
	init := strings.Join(cmds, "\n")
	for _, want := range []string{
		"iptables-nft -t filter -I OUTPUT 1 -j QUILL-EGRESS-OUT",
		"iptables-nft -t filter -I DOCKER-USER 1 -j QUILL-EGRESS-FWD",
		"iptables-nft -t nat -I PREROUTING 1 -j QUILL-EGRESS-NAT",
		"iptables-nft -t nat -I OUTPUT 1 -j QUILL-EGRESS-NAT",
		"--dports 15080,15443 -j DROP",
		"ip6tables-nft -t filter -I OUTPUT 1 -j QUILL-EGRESS-OUT",
	} {
		if !strings.Contains(init, want) {
			t.Errorf("init missing %q", want)
		}
	}

	cmds = nil
	if err := fw.Apply(true); err != nil {
		t.Fatal(err)
	}
	on := strings.Join(cmds, "\n")
	for _, want := range []string{
		"-t nat -A QUILL-EGRESS-NAT -p tcp --dport 443 -j REDIRECT --to-ports 15443",
		"-t nat -A QUILL-EGRESS-NAT -p tcp --dport 80 -j REDIRECT --to-ports 15080",
		"-t filter -A QUILL-EGRESS-OUT -d 169.254.0.0/16 -j DROP",
		"-t filter -A QUILL-EGRESS-OUT -m mark --mark 0x5155 -j RETURN",
		"-t filter -A QUILL-EGRESS-OUT -j REJECT",
		"-t filter -A QUILL-EGRESS-FWD -j REJECT",
	} {
		if !strings.Contains(on, want) {
			t.Errorf("enforce missing %q", want)
		}
	}
	// The metadata drop precedes the mark exemption: not even the proxy
	// reaches it.
	if strings.Index(on, "QUILL-EGRESS-OUT -d 169.254.0.0/16 -j DROP") > strings.Index(on, "QUILL-EGRESS-OUT -m mark --mark 0x5155 -j RETURN") {
		t.Error("metadata drop must come before the mark exemption")
	}
	// Reject is last in each filter chain.
	for _, chain := range []string{"QUILL-EGRESS-OUT", "QUILL-EGRESS-FWD"} {
		var last string
		for _, c := range cmds {
			if strings.HasPrefix(c, "iptables-nft") && strings.Contains(c, "-A "+chain+" ") {
				last = c
			}
		}
		if !strings.HasSuffix(last, "-j REJECT") {
			t.Errorf("%s ends with %q", chain, last)
		}
	}

	cmds = nil
	if err := fw.Apply(false); err != nil {
		t.Fatal(err)
	}
	for _, c := range cmds {
		if !strings.Contains(c, " -F ") {
			t.Errorf("allow-all should only flush, ran %q", c)
		}
	}
}
