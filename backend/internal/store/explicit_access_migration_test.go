package store_test

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/nielsuitterdijk22/quill/internal/store/db"
	"github.com/nielsuitterdijk22/quill/internal/store/migrations"
)

// TestExplicitAccessBackfill runs migration 000045 over an organization that
// still relies on implicit access: every current member must end up with the
// direct row the implicit rule used to give them (admin -> owner, member ->
// member), a stronger existing row must survive, and nobody gains access they
// did not have. Re-running must be harmless.
func TestExplicitAccessBackfill(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	pool := st.Pool()

	suffix := strings.ToLower(uuid.NewString()[:8])
	tenant, err := st.CreateTenant(ctx, db.CreateTenantParams{Slug: "xa-" + suffix, Name: "XA"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE tenants SET kind = 'org' WHERE id = $1`, tenant.ID); err != nil {
		t.Fatal(err)
	}
	mkProject := func(slug string) uuid.UUID {
		t.Helper()
		p, err := st.CreateProject(ctx, db.CreateProjectParams{TenantID: tenant.ID, Slug: slug + "-" + suffix, Name: slug})
		if err != nil {
			t.Fatal(err)
		}
		return p.ID
	}
	mkUser := func(name string) uuid.UUID {
		t.Helper()
		u, err := st.CreateUser(ctx, db.CreateUserParams{Username: name + suffix, Email: name + suffix + "@example.test", DisplayName: name, IsActive: true})
		if err != nil {
			t.Fatal(err)
		}
		return u.ID
	}
	p1, p2 := mkProject("p1"), mkProject("p2")
	admin, member, stranger := mkUser("admin"), mkUser("member"), mkUser("stranger")
	for u, role := range map[uuid.UUID]string{admin: "admin", member: "member"} {
		if _, err := pool.Exec(ctx, `INSERT INTO tenant_members (tenant_id, user_id, role) VALUES ($1, $2, $3)`, tenant.ID, u, role); err != nil {
			t.Fatal(err)
		}
	}
	// A pre-existing stronger row (member already owns p2) must not be downgraded.
	if _, err := pool.Exec(ctx, `INSERT INTO project_members (project_id, user_id, role) VALUES ($1, $2, 'owner')`, p2, member); err != nil {
		t.Fatal(err)
	}

	sqlBytes, err := migrations.FS.ReadFile("000045_explicit_access.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err := pool.Exec(ctx, string(sqlBytes)); err != nil {
			t.Fatalf("apply migration (run %d): %v", i+1, err)
		}
	}

	role := func(u, p uuid.UUID) string {
		t.Helper()
		var r string
		if err := pool.QueryRow(ctx, `SELECT COALESCE((SELECT role FROM project_access($1, $2)), '')`, u, p).Scan(&r); err != nil {
			t.Fatal(err)
		}
		return r
	}
	for _, c := range []struct {
		who  string
		user uuid.UUID
		proj uuid.UUID
		want string
	}{
		{"admin p1", admin, p1, "owner"},
		{"admin p2", admin, p2, "owner"},
		{"member p1", member, p1, "member"},
		{"member p2 (kept owner)", member, p2, "owner"},
		{"stranger p1", stranger, p1, ""},
	} {
		if got := role(c.user, c.proj); got != c.want {
			t.Errorf("%s = %q, want %q", c.who, got, c.want)
		}
	}

	// From here on membership alone is not access: a newcomer gets nothing.
	newcomer := mkUser("newcomer")
	if _, err := pool.Exec(ctx, `INSERT INTO tenant_members (tenant_id, user_id, role) VALUES ($1, $2, 'member')`, tenant.ID, newcomer); err != nil {
		t.Fatal(err)
	}
	if got := role(newcomer, p1); got != "" {
		t.Errorf("newcomer without a grant = %q, want none", got)
	}
}
