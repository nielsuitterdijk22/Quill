package platform

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

// These white-box integration tests cover pipeline secrets: project/repo/env
// scoping, admin-only authorization, write-only listing (names, not values),
// per-scope name uniqueness, and the project→repo→environment merge precedence
// resolveRunSecrets applies. They reuse the shared scopeTestService/seedScopeRepo
// helpers and are gated on QUILL_TEST_DATABASE_URL so CI without a database stays
// green.

func TestSecretSetAuthz(t *testing.T) {
	svc, st := scopeTestService(t)
	ctx := context.Background()
	owner, _ := seedScopeRepo(t, svc, st, "acme", "widget")

	// Owners (project admins) may set a secret.
	if _, err := svc.SetProjectSecret(ctx, owner, "acme", "API_TOKEN", "s3cret"); err != nil {
		t.Fatalf("owner set: %v", err)
	}

	// A non-member may neither list nor set secrets.
	stranger := Actor{UserID: scopeMakeUser(t, st, "secret-stranger")}
	if _, err := svc.ListProjectSecrets(ctx, stranger, "acme"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("stranger list: want ErrForbidden, got %v", err)
	}
	if _, err := svc.SetProjectSecret(ctx, stranger, "acme", "X", "y"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("stranger set: want ErrForbidden, got %v", err)
	}
}

func TestSecretListIsWriteOnly(t *testing.T) {
	svc, st := scopeTestService(t)
	ctx := context.Background()
	owner, _ := seedScopeRepo(t, svc, st, "acme", "widget")

	if _, err := svc.SetProjectSecret(ctx, owner, "acme", "API_TOKEN", "the-value"); err != nil {
		t.Fatalf("set: %v", err)
	}
	secrets, err := svc.ListProjectSecrets(ctx, owner, "acme")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(secrets) != 1 || secrets[0].Name != "API_TOKEN" {
		t.Fatalf("unexpected secrets: %+v", secrets)
	}
	// SecretSummary has no value field by construction — this test documents that
	// the listing carries only names and timestamps.
}

func TestSecretNameNormalizedAndValidated(t *testing.T) {
	svc, st := scopeTestService(t)
	ctx := context.Background()
	owner, _ := seedScopeRepo(t, svc, st, "acme", "widget")

	// Lower-case input is upper-cased; re-setting the same name (any case) updates
	// in place rather than creating a duplicate.
	if _, err := svc.SetProjectSecret(ctx, owner, "acme", "api_token", "v1"); err != nil {
		t.Fatalf("set lower: %v", err)
	}
	if _, err := svc.SetProjectSecret(ctx, owner, "acme", "API_TOKEN", "v2"); err != nil {
		t.Fatalf("update: %v", err)
	}
	secrets, _ := svc.ListProjectSecrets(ctx, owner, "acme")
	if len(secrets) != 1 || secrets[0].Name != "API_TOKEN" {
		t.Fatalf("want single API_TOKEN, got %+v", secrets)
	}

	// Reserved GITHUB_ prefix and malformed names are rejected.
	if _, err := svc.SetProjectSecret(ctx, owner, "acme", "GITHUB_TOKEN", "x"); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("GITHUB_ prefix: want ErrInvalidInput, got %v", err)
	}
	if _, err := svc.SetProjectSecret(ctx, owner, "acme", "1BAD", "x"); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("bad name: want ErrInvalidInput, got %v", err)
	}
	if _, err := svc.SetProjectSecret(ctx, owner, "acme", "OK_NAME", ""); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("empty value: want ErrInvalidInput, got %v", err)
	}
}

func TestSecretResolveMergePrecedence(t *testing.T) {
	svc, st := scopeTestService(t)
	ctx := context.Background()
	owner, repo := seedScopeRepo(t, svc, st, "acme", "widget")

	if _, err := svc.CreateEnvironment(ctx, owner, "acme", CreateEnvironmentInput{Slug: "production", Name: "Production"}); err != nil {
		t.Fatalf("create env: %v", err)
	}

	// Same name at all three scopes: environment must win over repo over project.
	if _, err := svc.SetProjectSecret(ctx, owner, "acme", "SHARED", "project"); err != nil {
		t.Fatalf("project secret: %v", err)
	}
	if _, err := svc.SetRepoSecret(ctx, owner, "acme", "widget", "SHARED", "repo"); err != nil {
		t.Fatalf("repo secret: %v", err)
	}
	if _, err := svc.SetEnvironmentSecret(ctx, owner, "acme", "production", "SHARED", "env"); err != nil {
		t.Fatalf("env secret: %v", err)
	}
	// A project-only secret should also be present.
	if _, err := svc.SetProjectSecret(ctx, owner, "acme", "PROJECT_ONLY", "p"); err != nil {
		t.Fatalf("project-only secret: %v", err)
	}

	env, err := svc.authorizedEnvironment(ctx, owner, "acme", "production")
	if err != nil {
		t.Fatalf("resolve env: %v", err)
	}
	envID := uuid.NullUUID{UUID: env.ID, Valid: true}

	// Without an environment: repo overrides project, no env layer.
	got, err := svc.resolveRunSecrets(ctx, repo.ProjectID, repo.ID, uuid.NullUUID{})
	if err != nil {
		t.Fatalf("resolve (no env): %v", err)
	}
	if got["SHARED"] != "repo" {
		t.Fatalf("no-env SHARED: want repo, got %q", got["SHARED"])
	}
	if got["PROJECT_ONLY"] != "p" {
		t.Fatalf("no-env PROJECT_ONLY: want p, got %q", got["PROJECT_ONLY"])
	}

	// With the environment: env overrides repo and project.
	got, err = svc.resolveRunSecrets(ctx, repo.ProjectID, repo.ID, envID)
	if err != nil {
		t.Fatalf("resolve (env): %v", err)
	}
	if got["SHARED"] != "env" {
		t.Fatalf("env SHARED: want env, got %q", got["SHARED"])
	}
	if got["PROJECT_ONLY"] != "p" {
		t.Fatalf("env PROJECT_ONLY: want p, got %q", got["PROJECT_ONLY"])
	}
}

func TestSecretDeleteRemovesFromResolve(t *testing.T) {
	svc, st := scopeTestService(t)
	ctx := context.Background()
	owner, repo := seedScopeRepo(t, svc, st, "acme", "widget")

	if _, err := svc.SetProjectSecret(ctx, owner, "acme", "GONE", "value"); err != nil {
		t.Fatalf("set: %v", err)
	}
	if err := svc.DeleteProjectSecret(ctx, owner, "acme", "gone"); err != nil {
		t.Fatalf("delete (case-insensitive): %v", err)
	}
	if err := svc.DeleteProjectSecret(ctx, owner, "acme", "GONE"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("delete missing: want ErrNotFound, got %v", err)
	}
	got, err := svc.resolveRunSecrets(ctx, repo.ProjectID, repo.ID, uuid.NullUUID{})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if _, ok := got["GONE"]; ok {
		t.Fatal("deleted secret still resolves")
	}
}
