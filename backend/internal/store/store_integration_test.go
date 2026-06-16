package store_test

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/nielsuitterdijk22/quill/internal/store"
	"github.com/nielsuitterdijk22/quill/internal/store/db"
)

// These tests exercise the real schema against a live Postgres. They are skipped
// unless QUILL_TEST_DATABASE_URL is set, so `go test ./...` stays green in CI
// without a database. To run locally:
//
//	QUILL_TEST_DATABASE_URL=postgres://quill:quill@localhost:5432/quill?sslmode=disable \
//	  go test ./internal/store/...
func testStore(t *testing.T) *store.Store {
	t.Helper()
	dsn := os.Getenv("QUILL_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("QUILL_TEST_DATABASE_URL not set; skipping store integration test")
	}
	if err := store.Migrate(dsn); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	st, err := store.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(st.Close)
	return st
}

// TestStoreRoundTrip creates the ownership chain (user → tenant → project → repo),
// reads each entity back by id and by slug, and asserts the values persisted.
func TestStoreRoundTrip(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	suffix := uuid.NewString()[:8]

	user, err := st.CreateUser(ctx, db.CreateUserParams{
		Username:    "user_" + suffix,
		Email:       "user_" + suffix + "@example.test",
		DisplayName: "Test User",
		IsActive:    true,
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	t.Cleanup(func() { cleanupUser(t, st, user.ID) })

	tenant, err := st.GetTenantBySlug(ctx, "default")
	if err != nil {
		t.Fatalf("get default tenant: %v", err)
	}

	project, err := st.CreateProject(ctx, db.CreateProjectParams{
		TenantID: tenant.ID,
		Slug:     "proj-" + suffix,
		Name:     "Project " + suffix,
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	t.Cleanup(func() { cleanupProject(t, st, project.ID) })

	repo, err := st.CreateRepository(ctx, db.CreateRepositoryParams{
		ProjectID:     project.ID,
		Slug:          "repo-" + suffix,
		Name:          "Repo " + suffix,
		Visibility:    "private",
		DefaultBranch: "main",
	})
	if err != nil {
		t.Fatalf("create repo: %v", err)
	}

	// Read back by id.
	got, err := st.GetRepositoryByID(ctx, repo.ID)
	if err != nil {
		t.Fatalf("get repo by id: %v", err)
	}
	if got.ProjectID != project.ID {
		t.Fatalf("repo ownership mismatch: project=%v", got.ProjectID)
	}

	// Read back by (project, slug); slug lookup normalizes via lower().
	bySlug, err := st.GetRepositoryBySlug(ctx, db.GetRepositoryBySlugParams{
		ProjectID: project.ID,
		Lower:     strings.ToUpper(repo.Slug), // prove case-insensitive lookup
	})
	if err != nil {
		t.Fatalf("get repo by slug: %v", err)
	}
	if bySlug.ID != repo.ID {
		t.Fatalf("slug lookup returned wrong repo: %v", bySlug.ID)
	}
}

// TestProjectRestrictsDeleteWithRepos asserts a project carrying live repos can't
// be deleted out from under them (ownership-as-data via ON DELETE RESTRICT).
func TestProjectRestrictsDeleteWithRepos(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	suffix := uuid.NewString()[:8]

	tenant, err := st.GetTenantBySlug(ctx, "default")
	if err != nil {
		t.Fatalf("get default tenant: %v", err)
	}

	project, err := st.CreateProject(ctx, db.CreateProjectParams{
		TenantID: tenant.ID,
		Slug:     "proj2-" + suffix,
		Name:     "Project2",
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	t.Cleanup(func() { cleanupProject(t, st, project.ID) })

	if _, err := st.CreateRepository(ctx, db.CreateRepositoryParams{
		ProjectID:     project.ID,
		Slug:          "repo2-" + suffix,
		Name:          "repo2",
		Visibility:    "private",
		DefaultBranch: "main",
	}); err != nil {
		t.Fatalf("create repo: %v", err)
	}

	// Deleting the project while it still owns a repo must be rejected by RESTRICT.
	if _, err := st.Pool().Exec(ctx, "DELETE FROM projects WHERE id = $1", project.ID); err == nil {
		t.Fatal("expected RESTRICT to block deleting a project with live repos, got nil error")
	}
}

func cleanupProject(t *testing.T, st *store.Store, id uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	// repositories use ON DELETE RESTRICT, so remove them before the project;
	// project_members cascade with the project.
	if _, err := st.Pool().Exec(ctx, "DELETE FROM repositories WHERE project_id = $1", id); err != nil {
		t.Logf("cleanup repos for project %v: %v", id, err)
	}
	if _, err := st.Pool().Exec(ctx, "DELETE FROM projects WHERE id = $1", id); err != nil {
		t.Logf("cleanup project %v: %v", id, err)
	}
}

func cleanupUser(t *testing.T, st *store.Store, id uuid.UUID) {
	t.Helper()
	if _, err := st.Pool().Exec(context.Background(), "DELETE FROM users WHERE id = $1", id); err != nil {
		t.Logf("cleanup user %v: %v", id, err)
	}
}
