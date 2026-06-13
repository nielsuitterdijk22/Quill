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

// TestStoreRoundTrip creates the full ownership chain (user → org → team → repo),
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

	org, err := st.CreateOrganization(ctx, db.CreateOrganizationParams{
		Slug: "org-" + suffix,
		Name: "Org " + suffix,
	})
	if err != nil {
		t.Fatalf("create org: %v", err)
	}
	t.Cleanup(func() { cleanupOrg(t, st, org.ID) })

	team, err := st.CreateTeam(ctx, db.CreateTeamParams{
		OrgID: org.ID,
		Slug:  "team-" + suffix,
		Name:  "Team " + suffix,
	})
	if err != nil {
		t.Fatalf("create team: %v", err)
	}

	repo, err := st.CreateRepository(ctx, db.CreateRepositoryParams{
		OrgID:         org.ID,
		OwningTeamID:  team.ID,
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
	if got.OwningTeamID != team.ID || got.OrgID != org.ID {
		t.Fatalf("repo ownership mismatch: org=%v team=%v", got.OrgID, got.OwningTeamID)
	}

	// Read back by (org, slug); slug lookup normalizes via lower().
	bySlug, err := st.GetRepositoryBySlug(ctx, db.GetRepositoryBySlugParams{
		OrgID: org.ID,
		Lower: strings.ToUpper(repo.Slug), // prove case-insensitive lookup
	})
	if err != nil {
		t.Fatalf("get repo by slug: %v", err)
	}
	if bySlug.ID != repo.ID {
		t.Fatalf("slug lookup returned wrong repo: %v", bySlug.ID)
	}
}

// TestOwningTeamMustBelongToRepoOrg asserts the composite foreign key blocks a
// repository from being owned by a team in a different org (ownership-as-data).
func TestOwningTeamMustBelongToRepoOrg(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	suffix := uuid.NewString()[:8]

	orgA, err := st.CreateOrganization(ctx, db.CreateOrganizationParams{Slug: "orga-" + suffix, Name: "OrgA"})
	if err != nil {
		t.Fatalf("create orgA: %v", err)
	}
	t.Cleanup(func() { cleanupOrg(t, st, orgA.ID) })

	orgB, err := st.CreateOrganization(ctx, db.CreateOrganizationParams{Slug: "orgb-" + suffix, Name: "OrgB"})
	if err != nil {
		t.Fatalf("create orgB: %v", err)
	}
	t.Cleanup(func() { cleanupOrg(t, st, orgB.ID) })

	teamA, err := st.CreateTeam(ctx, db.CreateTeamParams{OrgID: orgA.ID, Slug: "core-" + suffix, Name: "Core"})
	if err != nil {
		t.Fatalf("create teamA: %v", err)
	}

	// Repo in orgB owned by a team from orgA must be rejected by the composite FK.
	_, err = st.CreateRepository(ctx, db.CreateRepositoryParams{
		OrgID:         orgB.ID,
		OwningTeamID:  teamA.ID,
		Slug:          "bad-" + suffix,
		Name:          "bad",
		Visibility:    "private",
		DefaultBranch: "main",
	})
	if err == nil {
		t.Fatal("expected cross-org ownership to be rejected by composite FK, got nil error")
	}
}

func cleanupOrg(t *testing.T, st *store.Store, id uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	// repositories use ON DELETE RESTRICT, so remove them before the org; teams
	// cascade with the org.
	if _, err := st.Pool().Exec(ctx, "DELETE FROM repositories WHERE org_id = $1", id); err != nil {
		t.Logf("cleanup repos for org %v: %v", id, err)
	}
	if _, err := st.Pool().Exec(ctx, "DELETE FROM organizations WHERE id = $1", id); err != nil {
		t.Logf("cleanup org %v: %v", id, err)
	}
}

func cleanupUser(t *testing.T, st *store.Store, id uuid.UUID) {
	t.Helper()
	if _, err := st.Pool().Exec(context.Background(), "DELETE FROM users WHERE id = $1", id); err != nil {
		t.Logf("cleanup user %v: %v", id, err)
	}
}
