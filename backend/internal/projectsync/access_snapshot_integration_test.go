package projectsync_test

import (
	"context"
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nielsuitterdijk22/quill/internal/projectsync"
	"github.com/nielsuitterdijk22/quill/internal/store/db"
)

// Access changes reach Tempo as "access" events. Database triggers queue them (so
// no write path - SCIM, offboarding, group edits - can forget), one pending event
// per project however many changes land, and the payload is built from the data as
// it is when the event is delivered: the tenant roster plus each person's resolved
// role from project_access(), the same single definition Quill authorizes with.
func TestAccessEventsQueuedByTriggersAndSnapshotResolvesRoles(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := func(name string) db.User {
		u, err := st.CreateUser(ctx, db.CreateUserParams{Username: name + suffix, Email: name + suffix + "@example.invalid", DisplayName: name, IsActive: true})
		if err != nil {
			t.Fatal(err)
		}
		return u
	}
	admin, direct, viaGroup, plain := user("admin"), user("direct"), user("grouped"), user("plain")
	tenant, err := st.CreateOrgTenant(ctx, db.CreateOrgTenantParams{Slug: "as-" + suffix, Name: "AS"})
	if err != nil {
		t.Fatal(err)
	}
	for u, role := range map[uuid.UUID]string{admin.ID: "admin", direct.ID: "member", viaGroup.ID: "member", plain.ID: "member"} {
		if err := st.AddTenantMember(ctx, db.AddTenantMemberParams{TenantID: tenant.ID, UserID: u, Role: role}); err != nil {
			t.Fatal(err)
		}
	}
	project, err := st.CreateProject(ctx, db.CreateProjectParams{TenantID: tenant.ID, Slug: "as-p-" + suffix, Name: "P"})
	if err != nil {
		t.Fatal(err)
	}
	pending := func() int {
		var n int
		if err := st.Pool().QueryRow(ctx,
			`SELECT count(*) FROM project_sync_outbox WHERE project_id = $1 AND event_type = 'access' AND delivered_at IS NULL`,
			project.ID).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	deliverAll := func() {
		if _, err := st.Pool().Exec(ctx, `UPDATE project_sync_outbox SET delivered_at = now() WHERE project_id = $1 AND delivered_at IS NULL`, project.ID); err != nil {
			t.Fatal(err)
		}
	}
	deliverAll() // ignore whatever creating the fixtures queued

	// Several changes in a row queue one event, not one each.
	if err := st.AddProjectMember(ctx, db.AddProjectMemberParams{ProjectID: project.ID, UserID: direct.ID, Role: "member"}); err != nil {
		t.Fatal(err)
	}
	grp, err := st.CreateGroup(ctx, db.CreateGroupParams{TenantID: tenant.ID, Slug: "g" + suffix, Name: "G", Depth: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddGroupMember(ctx, db.AddGroupMemberParams{GroupID: grp.ID, UserID: viaGroup.ID}); err != nil {
		t.Fatal(err)
	}
	if err := st.SetGroupProjectRole(ctx, db.SetGroupProjectRoleParams{GroupID: grp.ID, ProjectID: project.ID, Role: "read"}); err != nil {
		t.Fatal(err)
	}
	if n := pending(); n != 1 {
		t.Fatalf("a burst of access changes must queue exactly one pending event, got %d", n)
	}

	// The snapshot: roster with tenant roles; resolved roles; an org admin has no
	// standing access; a tenant member with no grant is on the roster with no role.
	ev, found, err := projectsync.BuildAccessEvent(ctx, st, uuid.New(), project.ID, time.Now().UTC())
	if err != nil || !found {
		t.Fatalf("build: found=%v err=%v", found, err)
	}
	roles := map[string]string{}
	for _, g := range ev.Grants {
		roles[g.UserID.String()] = g.Role
	}
	tenantRoles := map[string]string{}
	for _, m := range ev.Members {
		tenantRoles[m.UserID.String()] = m.TenantRole
	}
	if len(ev.Members) != 4 || tenantRoles[admin.ID.String()] != "owner" || tenantRoles[plain.ID.String()] != "member" {
		t.Errorf("roster %+v", ev.Members)
	}
	if len(ev.Grants) != 2 || roles[direct.ID.String()] != "member" || roles[viaGroup.ID.String()] != "read" {
		t.Errorf("grants %+v (want direct=member, grouped=read, nobody else)", ev.Grants)
	}
	if _, ok := roles[admin.ID.String()]; ok {
		t.Error("an org admin must not appear with a grant: no standing access")
	}
	ids := make([]string, 0, len(ev.Members))
	for _, m := range ev.Members {
		ids = append(ids, m.Username)
	}
	if !sort.StringsAreSorted(ids) {
		t.Errorf("roster must be ordered by username for stable payloads: %v", ids)
	}

	// Once delivered, the next change queues a fresh event; offboarding someone
	// from the tenant does too, and drops them from the snapshot.
	deliverAll()
	if n := pending(); n != 0 {
		t.Fatalf("nothing pending after delivery, got %d", n)
	}
	if _, err := st.Pool().Exec(ctx, `DELETE FROM tenant_members WHERE tenant_id = $1 AND user_id = $2`, tenant.ID, direct.ID); err != nil {
		t.Fatal(err)
	}
	if n := pending(); n != 1 {
		t.Fatalf("offboarding must queue an access event, got %d", n)
	}
	ev, _, err = projectsync.BuildAccessEvent(ctx, st, uuid.New(), project.ID, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range ev.Members {
		if m.UserID == direct.ID {
			t.Error("an offboarded member must leave the snapshot")
		}
	}
	for _, g := range ev.Grants {
		if g.UserID == direct.ID {
			t.Error("an offboarded member's stale direct row must not resolve to a grant")
		}
	}

	// Changing a group's members queues events for every project it can reach.
	deliverAll()
	if err := st.RemoveGroupMember(ctx, db.RemoveGroupMemberParams{GroupID: grp.ID, UserID: viaGroup.ID}); err != nil {
		t.Fatal(err)
	}
	if n := pending(); n != 1 {
		t.Errorf("a group roster change must queue the projects it grants, got %d", n)
	}

	// A project that is gone yields no snapshot.
	if _, found, err := projectsync.BuildAccessEvent(ctx, st, uuid.New(), uuid.New(), time.Now().UTC()); err != nil || found {
		t.Errorf("unknown project: found=%v err=%v, want not found", found, err)
	}
}
