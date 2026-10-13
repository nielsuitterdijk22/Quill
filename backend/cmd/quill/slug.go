package main

import (
	"fmt"
	"regexp"
	"strings"
)

// normalizeSlug lowercases and trims a candidate slug.
func normalizeSlug(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// slugRe matches handles that are safe for both Quill and Forgejo: lowercase
// alphanumerics plus '-', '_' and '.', starting alphanumeric, up to 63 chars.
var slugRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,62}$`)

// reservedSlugs are handles Quill keeps for itself so project/repo slugs never
// collide with reserved frontend routes.
var reservedSlugs = map[string]bool{
	"new":          true,
	"edit":         true,
	"settings":     true,
	"api":          true,
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

// validSlug reports whether a normalized slug is well-formed and not reserved.
func validSlug(s string) bool {
	return slugRe.MatchString(s) && !reservedSlugs[s]
}

// normalizeRepoName turns a user-supplied repository name into a slug: it is
// lowercased, trimmed, and spaces/dots are mapped to dashes.
func normalizeRepoName(name string) (string, error) {
	s := strings.ToLower(strings.TrimSpace(name))
	s = strings.Map(func(r rune) rune {
		switch {
		case r == ' ' || r == '.':
			return '-'
		default:
			return r
		}
	}, s)
	if !validSlug(s) {
		return "", fmt.Errorf("invalid repository name %q: must be 1-63 chars of lowercase letters, digits, '-', '_' or '.', start alphanumeric, and not be a reserved word", name)
	}
	return s, nil
}

func validVisibility(v string) bool {
	switch v {
	case "public", "internal", "private":
		return true
	default:
		return false
	}
}
