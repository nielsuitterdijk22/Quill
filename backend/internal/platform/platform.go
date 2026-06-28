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
	"admin":        true,
	"sign-in":      true,
	"sign-up":      true,
	"login":        true,
	"register":     true,
}

// normalizeSlug lowercases and trims a candidate slug.
func normalizeSlug(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// validSlug reports whether a normalized slug is well-formed and not reserved.
func validSlug(s string) bool {
	return slugRe.MatchString(s) && !reservedSlugs[s]
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
