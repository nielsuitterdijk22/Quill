package platform

import (
	"context"
	"errors"
	"testing"
)

// TestSetUsername covers handle validation, uniqueness, and the onboarding-only
// gate. Forgejo is disabled in scopeTestService, so the Forgejo rename path is
// skipped here (it's exercised against a live instance). DB-gated.
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

	// Once the user owns a project, the handle is locked.
	if _, err := svc.CreateProject(ctx, Actor{UserID: uid}, CreateProjectInput{Slug: "acme", Name: "acme"}); err != nil {
		t.Fatalf("create project: %v", err)
	}
	if _, err := svc.SetUsername(ctx, uid, "niels2"); !errors.Is(err, ErrConflict) {
		t.Fatalf("change after project exists: want ErrConflict, got %v", err)
	}
}
