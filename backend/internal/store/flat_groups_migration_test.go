package store_test

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/nielsuitterdijk22/quill/internal/store/db"
	"github.com/nielsuitterdijk22/quill/internal/store/migrations"
)

// TestFlatGroupsMigrationKeepsAccess runs migration 000044 over a legacy nested
// tree: roles granted to a group must reach every descendant explicitly (so
// flattening changes nobody's access), the strongest role wins where a
// descendant already had its own grant, and the tree is cleared (parent_id NULL,
// depth 1) so deleting a group can no longer cascade into former children.
// The migration is applied as plain SQL over the already-migrated schema, which
// also proves it can be re-run.
func TestFlatGroupsMigrationKeepsAccess(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	pool := st.Pool()

	suffix := strings.ToLower(uuid.NewString()[:8])
	tenant, err := st.CreateTenant(ctx, db.CreateTenantParams{Slug: "flat-" + suffix, Name: "Flat"})
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
	p1, p2 := mkProject("p1"), mkProject("p2")
	user, err := st.CreateUser(ctx, db.CreateUserParams{Username: "flat" + suffix, Email: "flat" + suffix + "@example.test", DisplayName: "flat", IsActive: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO tenant_members (tenant_id, user_id, role) VALUES ($1, $2, 'member')`, tenant.ID, user.ID); err != nil {
		t.Fatal(err)
	}

	// root → mid → leaf (a legacy 3-level tree).
	mkGroup := func(slug string, parent *uuid.UUID, depth int) uuid.UUID {
		t.Helper()
		var id uuid.UUID
		if err := pool.QueryRow(ctx, `INSERT INTO groups (tenant_id, parent_id, slug, name, depth) VALUES ($1, $2, $3, $3, $4) RETURNING id`,
			tenant.ID, parent, slug, depth).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	root := mkGroup("root", nil, 1)
	mid := mkGroup("mid", &root, 2)
	leaf := mkGroup("leaf", &mid, 3)
	grant := func(g, p uuid.UUID, role string) {
		t.Helper()
		if _, err := pool.Exec(ctx, `INSERT INTO group_project_roles (group_id, project_id, role) VALUES ($1, $2, $3)`, g, p, role); err != nil {
			t.Fatal(err)
		}
	}
	grant(root, p1, "owner")  // reaches mid and leaf
	grant(mid, p2, "member")  // reaches leaf only
	grant(leaf, p1, "member") // leaf already has a lower grant on p1: owner must win
	if _, err := pool.Exec(ctx, `INSERT INTO group_members (group_id, user_id) VALUES ($1, $2)`, leaf, user.ID); err != nil {
		t.Fatal(err)
	}

	role := func(project uuid.UUID) string {
		t.Helper()
		var r string
		if err := pool.QueryRow(ctx, `SELECT COALESCE((SELECT role FROM project_access($1, $2)), '')`, user.ID, project).Scan(&r); err != nil {
			t.Fatal(err)
		}
		return r
	}
	groupRole := func(g, p uuid.UUID) string {
		t.Helper()
		var r string
		err := pool.QueryRow(ctx, `SELECT COALESCE((SELECT role FROM group_project_roles WHERE group_id = $1 AND project_id = $2), '')`, g, p).Scan(&r)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	// The schema is already at the latest version, so the resolver is flat: until
	// the data statements of the migration run, the leaf member holds only the
	// leaf group's own grant. (A database upgraded from the nested world has the
	// ancestors' roles copied by exactly those statements.)
	if got := role(p1); got != "member" {
		t.Fatalf("before: leaf member on p1 = %q, want member (own grant only)", got)
	}

	sqlBytes, err := migrations.FS.ReadFile("000044_flat_groups.up.sql")
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	for i := 0; i < 2; i++ { // twice: re-running must be harmless
		if _, err := pool.Exec(ctx, string(sqlBytes)); err != nil {
			t.Fatalf("apply migration (run %d): %v", i+1, err)
		}
	}

	// The leaf member keeps what the tree used to give them, now explicit on the leaf group.
	if got := role(p1); got != "owner" {
		t.Errorf("after: p1 = %q, want owner", got)
	}
	if got := role(p2); got != "member" {
		t.Errorf("after: p2 = %q, want member", got)
	}
	if got := groupRole(leaf, p1); got != "owner" {
		t.Errorf("leaf grant on p1 = %q, want owner (max of inherited owner and own member)", got)
	}
	if got := groupRole(mid, p1); got != "owner" {
		t.Errorf("mid grant on p1 = %q, want owner (copied from root)", got)
	}
	if got := groupRole(mid, p2); got != "member" {
		t.Errorf("mid grant on p2 = %q, want member (its own grant is untouched)", got)
	}
	if got := groupRole(root, p2); got != "" {
		t.Errorf("root must not gain a grant from its descendants, got %q", got)
	}

	// The tree is gone: nothing can cascade from a parent to a former child.
	var nested int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM groups WHERE tenant_id = $1 AND (parent_id IS NOT NULL OR depth <> 1)`, tenant.ID).Scan(&nested); err != nil || nested != 0 {
		t.Errorf("groups still nested: %d (err %v)", nested, err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM groups WHERE id = $1`, root); err != nil {
		t.Fatal(err)
	}
	var left int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM groups WHERE id IN ($1, $2)`, mid, leaf).Scan(&left); err != nil || left != 2 {
		t.Errorf("deleting the former root removed its former children: %d of 2 left (err %v)", left, err)
	}

	// Resolver is flat now: a grant on a group no longer reaches its (former)
	// descendants through anything but the copied rows.
	if _, err := pool.Exec(ctx, `UPDATE groups SET parent_id = $1 WHERE id = $2`, mid, leaf); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM group_project_roles WHERE group_id = $1`, leaf); err != nil {
		t.Fatal(err)
	}
	// (Org members hold an implicit 'member' role for now, so the ancestor's
	// grant is made 'owner' to tell inheritance from the implicit role.)
	if _, err := pool.Exec(ctx, `UPDATE group_project_roles SET role = 'owner' WHERE group_id = $1 AND project_id = $2`, mid, p2); err != nil {
		t.Fatal(err)
	}
	if got := role(p2); got != "member" {
		t.Errorf("resolver must ignore parent_id: p2 via an ancestor's owner grant = %q, want member (the implicit org role)", got)
	}
}
