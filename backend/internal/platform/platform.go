// Package platform contains Quill's domain services that sit above the data
// store and the Forgejo client. They own the write-through choreography that
// keeps Quill's Postgres metadata and Forgejo's git-side objects in agreement:
// projects and repositories are created in Forgejo first, then recorded in
// Postgres, and Forgejo state is compensated (deleted) if the local transaction
// fails.
package platform

import (
	"errors"
	"regexp"
	"strings"
)

// Sentinel errors mapped to HTTP status codes by the server layer.
var (
	ErrInvalidInput = errors.New("invalid input")
	ErrConflict     = errors.New("already exists")
	ErrNotFound     = errors.New("not found")
	ErrForbidden    = errors.New("forbidden")
	// ErrUnavailable signals that a git-side operation can't be served because
	// Forgejo is disabled or the resource isn't linked to a git repository.
	ErrUnavailable = errors.New("git backend unavailable")
	// ErrPolicyViolation signals that an operation is blocked by a branch policy
	// (e.g. merging a pull request that lacks the required approvals).
	ErrPolicyViolation = errors.New("blocked by branch policy")
	// ErrEmptyRepo signals that the git repository exists but has no commits yet,
	// so content/branch/commit reads cannot be served.
	ErrEmptyRepo = errors.New("repository is empty")
	// ErrLimitReached signals that the tenant's plan quota is exhausted
	// (repositories, storage, or pipeline minutes on the free plan) and the
	// operation was rejected. The free plan is the freemium premium gate.
	ErrLimitReached = errors.New("plan limit reached")
	// ErrPaymentsNotConfigured signals that the Creem payment API has no
	// credentials on this deployment — plan upgrades aren't purchasable yet.
	ErrPaymentsNotConfigured = errors.New("payments not configured")
)

// Visibility values accepted for repositories. "internal" is visible to all
// authenticated platform users; it maps to a private repo in Forgejo today.
const (
	VisibilityPublic   = "public"
	VisibilityInternal = "internal"
	VisibilityPrivate  = "private"
)

// defaultTenantSlug is the seeded billing/SSO boundary every project attaches to
// until multi-tenant management lands.
const defaultTenantSlug = "default"

// slugRe matches handles that are safe for both Quill and Forgejo: lowercase
// alphanumerics plus '-', '_' and '.', starting alphanumeric, up to 63 chars.
var slugRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,62}$`)

// reservedSlugs are handles Quill keeps for itself so project/repo slugs never
// collide with reserved frontend routes. Personal namespaces sit at /{slug}, so
// top-level app routes must be included here.
var reservedSlugs = map[string]bool{
	// Repo/project sub-routes
	"new":      true,
	"edit":     true,
	"settings": true,
	"api":      true,
	// Top-level frontend app routes (personal namespaces live at /{slug})
	"projects":     true,
	"repositories": true,
	"pulls":        true,
	"pipelines":    true,
	"sbom":         true,
	"admin":        true,
	"sign-in":      true,
	"sign-up":      true,
	"login":        true,
	"register":     true,
	"orgs":         true,
	"invite":       true,
	"beta":         true,
	"onboarding":   true,
}

// normalizeSlug lowercases and trims a candidate slug.
func normalizeSlug(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// validSlug reports whether a normalized slug is well-formed and not reserved.
// Beyond slugRe it rejects what git hosting trips over: a trailing '.', '..',
// and the ".git"/".atom" suffixes Forgejo reserves.
func validSlug(s string) bool {
	if !slugRe.MatchString(s) || reservedSlugs[s] {
		return false
	}
	return !strings.HasSuffix(s, ".") && !strings.Contains(s, "..") &&
		!strings.HasSuffix(s, ".git") && !strings.HasSuffix(s, ".atom")
}

// ValidHandle reports whether s (already lowercase) is an acceptable username:
// the same rule as every slug, so the handle can be a URL namespace.
func ValidHandle(s string) bool { return validSlug(s) }

var slugJunkRe = regexp.MustCompile(`[^a-z0-9._-]+`)

// SlugFromName derives a valid slug from free text (the UI never asks people
// to type slugs): lowercase, runs of other characters become '-', separators
// trimmed from the ends, at most 63 chars. It may return "" or a reserved word
// for hopeless input; callers still run validSlug.
func SlugFromName(name string) string {
	s := slugJunkRe.ReplaceAllString(strings.ToLower(strings.TrimSpace(name)), "-")
	for strings.Contains(s, "..") {
		s = strings.ReplaceAll(s, "..", ".")
	}
	if len(s) > 63 {
		s = s[:63]
	}
	s = strings.Trim(s, "-._")
	for _, suffix := range []string{".git", ".atom"} {
		s = strings.Trim(strings.TrimSuffix(s, suffix), "-._")
	}
	return s
}

func validVisibility(v string) bool {
	switch v {
	case VisibilityPublic, VisibilityInternal, VisibilityPrivate:
		return true
	default:
		return false
	}
}

// forgejoPrivate reports whether a Quill visibility maps to a private Forgejo repo.
func forgejoPrivate(visibility string) bool {
	return visibility != VisibilityPublic
}
