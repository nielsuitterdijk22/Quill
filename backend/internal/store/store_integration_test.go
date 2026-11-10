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

// TestTenantPlanColumn asserts the tenants.plan column (migration 000019)
// and migration 000021's freemium default: the seeded default tenant keeps the
// operator's 'internal' plan, while newly created tenants start on 'free'.
func TestTenantPlanColumn(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()

	// The seeded default tenant must carry the default plan.
	tenant, err := st.GetTenantBySlug(ctx, "default")
	if err != nil {
		t.Fatalf("get default tenant: %v", err)
	}
	if tenant.Plan != "internal" {
		t.Fatalf("default tenant plan = %q, want %q", tenant.Plan, "internal")
	}

	// A freshly created tenant inherits the default plan.
	suffix := uuid.NewString()[:8]
	created, err := st.CreateTenant(ctx, db.CreateTenantParams{
		Slug: "plan-" + suffix,
		Name: "Plan " + suffix,
	})
	if err != nil {
		t.Fatalf("create tenant: %v", err)
	}
	t.Cleanup(func() {
		if err := st.DeleteTenant(context.Background(), created.ID); err != nil {
			t.Logf("cleanup tenant %v: %v", created.ID, err)
		}
	})
	if created.Plan != "free" {
		t.Fatalf("created tenant plan = %q, want %q", created.Plan, "free")
	}

	// Read back by id: the plan must round-trip through GetTenantByID.
	got, err := st.GetTenantByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("get tenant by id: %v", err)
	}
	if got.Plan != "free" {
		t.Fatalf("tenant plan by id = %q, want %q", got.Plan, "free")
	}
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

// TestAuditLogTenantScoping pins the hard isolation rule: audit log reads are
// scoped to the viewer's own tenant, so no admin (platform admin included) can
// see another tenant's entries, and legacy NULL-tenant entries are visible only
// to legacy NULL-tenant accounts.
func TestAuditLogTenantScoping(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()

	tenantA, err := st.CreateTenant(ctx, db.CreateTenantParams{Slug: "audit-a-" + strings.ToLower(uuid.NewString()[:8]), Name: "Tenant A"})
	if err != nil {
		t.Fatalf("create tenant A: %v", err)
	}
	tenantB, err := st.CreateTenant(ctx, db.CreateTenantParams{Slug: "audit-b-" + strings.ToLower(uuid.NewString()[:8]), Name: "Tenant B"})
	if err != nil {
		t.Fatalf("create tenant B: %v", err)
	}
	t.Cleanup(func() {
		_, _ = st.Pool().Exec(ctx, "DELETE FROM tenants WHERE id IN ($1, $2)", tenantA.ID, tenantB.ID)
	})

	userIn := func(t *testing.T, name string, tenant uuid.NullUUID) uuid.UUID {
		t.Helper()
		u, err := st.CreateUser(ctx, db.CreateUserParams{Username: name, Email: name + "@example.test", DisplayName: name, IsAdmin: true})
		if err != nil {
			t.Fatalf("create user %s: %v", name, err)
		}
		t.Cleanup(func() { cleanupUser(t, st, u.ID) })
		if tenant.Valid {
			if _, err := st.Pool().Exec(ctx, "UPDATE users SET tenant_id = $1 WHERE id = $2", tenant.UUID, u.ID); err != nil {
				t.Fatalf("set tenant for %s: %v", name, err)
			}
		}
		return u.ID
	}

	adminA := userIn(t, "audit-admin-a-"+uuid.NewString()[:8], uuid.NullUUID{UUID: tenantA.ID, Valid: true})
	otherB := userIn(t, "audit-user-b-"+uuid.NewString()[:8], uuid.NullUUID{UUID: tenantB.ID, Valid: true})
	legacy := userIn(t, "audit-legacy-"+uuid.NewString()[:8], uuid.NullUUID{})

	insert := func(actor uuid.UUID, action string, tenant uuid.NullUUID) int64 {
		t.Helper()
		e, err := st.InsertAuditLog(ctx, db.InsertAuditLogParams{
			ActorUserID:   uuid.NullUUID{UUID: actor, Valid: true},
			Action:        action,
			TargetType:    "user",
			TargetID:      actor.String(),
			Metadata:      []byte(`{}`),
			ActorUsername: action,
			TenantID:      tenant,
		})
		if err != nil {
			t.Fatalf("insert audit %s: %v", action, err)
		}
		return e.ID
	}

	idA := insert(adminA, "auth.sign_in", uuid.NullUUID{UUID: tenantA.ID, Valid: true})
	idB := insert(otherB, "auth.sign_in", uuid.NullUUID{UUID: tenantB.ID, Valid: true})
	idL := insert(legacy, "auth.sign_in", uuid.NullUUID{})

	// Tenant A's admin sees only tenant A entries.
	got, err := st.ListAuditLogFiltered(ctx, db.ListAuditLogFilteredParams{
		TenantID: uuid.NullUUID{UUID: tenantA.ID, Valid: true},
		Limit:    100,
		Offset:   0,
	})
	if err != nil {
		t.Fatalf("list for tenant A: %v", err)
	}
	ids := map[int64]bool{}
	for _, e := range got {
		ids[e.ID] = true
	}
	if ids[idA] != true || ids[idB] == true || ids[idL] == true {
		t.Fatalf("tenant A view leaked: a=%v b=%v legacy=%v", ids[idA], ids[idB], ids[idL])
	}

	// Tenant B's view is likewise isolated.
	got, err = st.ListAuditLogFiltered(ctx, db.ListAuditLogFilteredParams{
		TenantID: uuid.NullUUID{UUID: tenantB.ID, Valid: true},
		Limit:    100,
		Offset:   0,
	})
	if err != nil {
		t.Fatalf("list for tenant B: %v", err)
	}
	if len(got) != 1 || got[0].ID != idB {
		t.Fatalf("tenant B view: got %d entries, want exactly its own", len(got))
	}

	// A legacy (NULL tenant) admin sees only NULL-tenant entries.
	got, err = st.ListAuditLogFiltered(ctx, db.ListAuditLogFilteredParams{
		TenantID: uuid.NullUUID{},
		Limit:    100,
		Offset:   0,
	})
	if err != nil {
		t.Fatalf("list for legacy: %v", err)
	}
	for _, e := range got {
		if e.TenantID.Valid {
			t.Fatalf("legacy view leaked entry %d from tenant %v", e.ID, e.TenantID.UUID)
		}
	}
	foundL := false
	for _, e := range got {
		if e.ID == idL {
			foundL = true
		}
	}
	if !foundL {
		t.Fatalf("legacy view missing its own entry")
	}
}
