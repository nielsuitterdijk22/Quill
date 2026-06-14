// Package platform contains Quill's domain services that sit above the data
// store and the Forgejo client. They own the write-through choreography that
// keeps Quill's Postgres metadata and Forgejo's git-side objects in agreement:
// organizations and repositories are created in Forgejo first, then recorded in
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
)

// Visibility values accepted for repositories. "internal" is visible to all
// authenticated platform users; it maps to a private repo in Forgejo today.
const (
	VisibilityPublic   = "public"
	VisibilityInternal = "internal"
	VisibilityPrivate  = "private"
)

// defaultOwningTeamSlug is the team created with every org; new repos default to
// it as their required owning team.
const defaultOwningTeamSlug = "owners"

// slugRe matches handles that are safe for both Quill and Forgejo: lowercase
// alphanumerics plus '-', '_' and '.', starting alphanumeric, up to 63 chars.
var slugRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,62}$`)

// reservedSlugs are handles Quill keeps for itself so org/repo slugs never
// collide with reserved frontend routes (e.g. the "/orgs/new" create form or a
// repo's "/new" create form).
var reservedSlugs = map[string]bool{
	"new":      true,
	"edit":     true,
	"settings": true,
	"api":      true,
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
