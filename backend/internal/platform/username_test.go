package platform

import (
	"context"
	"errors"
	"testing"
)

// TestSetUsername covers handle validation, uniqueness, and the rename after
// projects exist (allowed since DECISIONS.md 2026-11-06; the personal project
// slug follows the new handle). Forgejo is disabled in scopeTestService, so
// the Forgejo rename path is skipped here (it's exercised against a live
// instance). DB-gated.
func TestSetUsername(t *testing.T) {
	svc, st := scopeTestService(t)
	ctx := context.Background()
	uid := scopeMakeUser(t, st, "derived-name")

	// Bad format and reserved words are rejected.
	for _, bad := range []string{"Bad Name!", "admin", "-leading", ""} {
		if _, err := svc.SetUsername(ctx, uid, bad); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("SetUsername(%q): want ErrInvalidInput, got %v", bad, err)
		}
	}

	// A valid change succeeds.
	u, err := svc.SetUsername(ctx, uid, "niels")
	if err != nil {
		t.Fatalf("SetUsername: %v", err)
	}
	if u.Username != "niels" {
		t.Fatalf("username = %q, want niels", u.Username)
	}

	// Another user can't take it (case-insensitive).
	uid2 := scopeMakeUser(t, st, "other")
	if _, err := svc.SetUsername(ctx, uid2, "Niels"); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate username: want ErrConflict, got %v", err)
	}

	// Renames stay allowed once projects exist (the old onboarding-only guard
	// was removed); the personal project's slug follows the new handle.
	if err := svc.CreatePersonalProject(ctx, uid, "niels"); err != nil {
		t.Fatalf("create personal project: %v", err)
	}
	if _, err := svc.CreateProject(ctx, Actor{UserID: uid}, CreateProjectInput{Slug: "acme", Name: "acme"}); err != nil {
		t.Fatalf("create project: %v", err)
	}
	u2, err := svc.SetUsername(ctx, uid, "niels2")
	if err != nil {
		t.Fatalf("rename after projects exist: %v", err)
	}
	if u2.Username != "niels2" {
		t.Fatalf("username = %q, want niels2", u2.Username)
	}
	projects, err := st.ListProjectsByUser(ctx, uid)
	if err != nil {
		t.Fatalf("list projects: %v", err)
	}
	seen := map[string]bool{}
	for _, p := range projects {
		seen[p.Slug] = p.IsPersonal
	}
	if personal, ok := seen["niels2"]; !ok || !personal {
		t.Fatalf("personal project slug should follow the rename to niels2, got %+v", seen)
	}
	if _, ok := seen["acme"]; !ok {
		t.Fatalf("non-personal project acme must be untouched, got %+v", seen)
	}
}
