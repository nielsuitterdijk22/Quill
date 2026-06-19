package platform

import (
	"context"
	"errors"
	"testing"
)

// These white-box integration tests cover the environments entity: project-scoped
// CRUD, authorization (member reads, admin writes), the promotion-rank ordering,
// slug uniqueness, and validation. They reuse the shared scopeTestService/
// seedScopeRepo helpers (policies_scope_test.go) and are gated on
// QUILL_TEST_DATABASE_URL so CI without a database stays green.

func TestEnvironmentCreateAuthz(t *testing.T) {
	svc, st := scopeTestService(t)
	ctx := context.Background()
	owner, _ := seedScopeRepo(t, svc, st, "acme", "widget")

	// Owners (project admins) may create environments.
	if _, err := svc.CreateEnvironment(ctx, owner, "acme", CreateEnvironmentInput{Slug: "staging", Name: "Staging", Rank: 0}); err != nil {
		t.Fatalf("owner create: %v", err)
	}

	// A non-member may neither read nor create environments.
	stranger := Actor{UserID: scopeMakeUser(t, st, "env-entity-stranger")}
	if _, _, err := svc.ListEnvironments(ctx, stranger, "acme"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("stranger list: want ErrForbidden, got %v", err)
	}
	if _, err := svc.CreateEnvironment(ctx, stranger, "acme", CreateEnvironmentInput{Slug: "prod"}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("stranger create: want ErrForbidden, got %v", err)
	}
}

func TestEnvironmentListOrdersByRank(t *testing.T) {
	svc, st := scopeTestService(t)
	ctx := context.Background()
	owner, _ := seedScopeRepo(t, svc, st, "acme", "widget")

	// Insert out of rank order; the list must come back ladder-ordered.
	if _, err := svc.CreateEnvironment(ctx, owner, "acme", CreateEnvironmentInput{Slug: "production", Name: "Production", Rank: 2}); err != nil {
		t.Fatalf("create prod: %v", err)
	}
	if _, err := svc.CreateEnvironment(ctx, owner, "acme", CreateEnvironmentInput{Slug: "staging", Name: "Staging", Rank: 1}); err != nil {
		t.Fatalf("create staging: %v", err)
	}
	if _, err := svc.CreateEnvironment(ctx, owner, "acme", CreateEnvironmentInput{Slug: "dev", Name: "Dev", Rank: 0}); err != nil {
		t.Fatalf("create dev: %v", err)
	}

	_, envs, err := svc.ListEnvironments(ctx, owner, "acme")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(envs) != 3 {
		t.Fatalf("want 3 environments, got %d", len(envs))
	}
	if envs[0].Slug != "dev" || envs[1].Slug != "staging" || envs[2].Slug != "production" {
		t.Fatalf("rank order wrong: %s, %s, %s", envs[0].Slug, envs[1].Slug, envs[2].Slug)
	}
}

func TestEnvironmentDuplicateSlugConflicts(t *testing.T) {
	svc, st := scopeTestService(t)
	ctx := context.Background()
	owner, _ := seedScopeRepo(t, svc, st, "acme", "widget")

	if _, err := svc.CreateEnvironment(ctx, owner, "acme", CreateEnvironmentInput{Slug: "staging"}); err != nil {
		t.Fatalf("first create: %v", err)
	}
	if _, err := svc.CreateEnvironment(ctx, owner, "acme", CreateEnvironmentInput{Slug: "Staging"}); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate slug: want ErrConflict, got %v", err)
	}
}

func TestEnvironmentUpdateAndDelete(t *testing.T) {
	svc, st := scopeTestService(t)
	ctx := context.Background()
	owner, _ := seedScopeRepo(t, svc, st, "acme", "widget")

	if _, err := svc.CreateEnvironment(ctx, owner, "acme", CreateEnvironmentInput{Slug: "staging", Name: "Staging", Rank: 0}); err != nil {
		t.Fatalf("create: %v", err)
	}

	updated, err := svc.UpdateEnvironment(ctx, owner, "acme", "staging", UpdateEnvironmentInput{
		Name:        "Staging EU",
		Description: "EU soak environment",
		Rank:        5,
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Name != "Staging EU" || updated.Rank != 5 || updated.Slug != "staging" {
		t.Fatalf("update did not apply: %+v", updated)
	}

	got, err := svc.GetEnvironment(ctx, owner, "acme", "staging")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Description != "EU soak environment" {
		t.Fatalf("description not persisted: %+v", got)
	}

	if err := svc.DeleteEnvironment(ctx, owner, "acme", "staging"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := svc.GetEnvironment(ctx, owner, "acme", "staging"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("get after delete: want ErrNotFound, got %v", err)
	}
	if err := svc.DeleteEnvironment(ctx, owner, "acme", "staging"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("delete missing: want ErrNotFound, got %v", err)
	}
}

func TestEnvironmentValidation(t *testing.T) {
	svc, st := scopeTestService(t)
	ctx := context.Background()
	owner, _ := seedScopeRepo(t, svc, st, "acme", "widget")

	// Invalid slug.
	if _, err := svc.CreateEnvironment(ctx, owner, "acme", CreateEnvironmentInput{Slug: "Not A Slug"}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("bad slug: want ErrInvalidInput, got %v", err)
	}
	// Negative rank.
	if _, err := svc.CreateEnvironment(ctx, owner, "acme", CreateEnvironmentInput{Slug: "staging", Rank: -1}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("negative rank: want ErrInvalidInput, got %v", err)
	}
}
