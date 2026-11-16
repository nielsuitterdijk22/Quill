package platform

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nielsuitterdijk22/quill/internal/store/db"
)

// Groups + billing integration tests, gated on QUILL_TEST_DATABASE_URL like the
// other scope tests: nested group hierarchy with depth cap, group→project role
// inheritance into the authorization checks, and usage metering / plan changes.

func TestNestedGroupHierarchy(t *testing.T) {
	svc, st := scopeTestService(t)
	ctx := context.Background()
	founder := Actor{UserID: scopeMakeUser(t, st, "gfounder")}

	tenant, _, err := svc.CreateOrganization(ctx, founder, "bank", "Bank")
	if err != nil {
		t.Fatalf("create org: %v", err)
	}

	// Build a chain to the max depth: depth 1..5 succeed.
	parent := ""
	for i := 1; i <= MaxGroupDepth; i++ {
		slug := "level" + string(rune('0'+i))
		g, err := svc.CreateGroup(ctx, founder, "bank", slug, "Level "+string(rune('0'+i)), parent)
		if err != nil {
			t.Fatalf("create group depth %d: %v", i, err)
		}
		if g.Depth != int32(i) {
			t.Fatalf("depth %d: got depth %d", i, g.Depth)
		}
		parent = slug
	}
	// Depth 6 is rejected.
	if _, err := svc.CreateGroup(ctx, founder, "bank", "toodeep", "Too deep", parent); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("depth 6: want ErrInvalidInput, got %v", err)
	}

	// A non-admin org member cannot create groups.
	member := Actor{UserID: scopeMakeUser(t, st, "gmember0")}
	if err := svc.store.AddTenantMember(ctx, db.AddTenantMemberParams{TenantID: tenant.ID, UserID: member.UserID, Role: "member"}); err != nil {
		t.Fatalf("add tenant member: %v", err)
	}
	if _, err := svc.CreateGroup(ctx, member, "bank", "nope", "Nope", ""); !errors.Is(err, ErrForbidden) {
		t.Fatalf("member create: want ErrForbidden, got %v", err)
	}
	// ...but can read the tree.
	if groups, err := svc.ListGroups(ctx, member, "bank"); err != nil || len(groups) != MaxGroupDepth {
		t.Fatalf("member list: want %d groups, got %d (err %v)", MaxGroupDepth, len(groups), err)
	}

	// Duplicate slug conflicts.
	if _, err := svc.CreateGroup(ctx, founder, "bank", "level1", "Dup", ""); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate slug: want ErrConflict, got %v", err)
	}
	// Invalid slug rejected.
	if _, err := svc.CreateGroup(ctx, founder, "bank", "Bad Slug!", "Bad", ""); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("invalid slug: want ErrInvalidInput, got %v", err)
	}

	// Deleting the root cascades the whole subtree.
	if err := svc.DeleteGroup(ctx, founder, "bank", "level1"); err != nil {
		t.Fatalf("delete root: %v", err)
	}
	if groups, err := svc.ListGroups(ctx, founder, "bank"); err != nil || len(groups) != 0 {
		t.Fatalf("after delete: want 0 groups, got %d (err %v)", len(groups), err)
	}
}

func TestGroupProjectRoleInheritance(t *testing.T) {
	svc, st := scopeTestService(t)
	ctx := context.Background()
	founder := Actor{UserID: scopeMakeUser(t, st, "ifounder")}

	_, project, err := svc.CreateOrganization(ctx, founder, "acme", "Acme")
	if err != nil {
		t.Fatalf("create org: %v", err)
	}

	// dept (depth 1) → team (depth 2). The user joins only the subgroup.
	if _, err := svc.CreateGroup(ctx, founder, "acme", "dept", "Department", ""); err != nil {
		t.Fatalf("create dept: %v", err)
	}
	if _, err := svc.CreateGroup(ctx, founder, "acme", "team", "Team", "dept"); err != nil {
		t.Fatalf("create team: %v", err)
	}
	memberID := scopeMakeUser(t, st, "imember")
	if err := svc.AddGroupMember(ctx, founder, "acme", "team", memberID); err != nil {
		t.Fatalf("add group member: %v", err)
	}
	member := Actor{UserID: memberID}

	// No grant yet: no access.
	if err := svc.authorizeProjectMember(ctx, member, project.ID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("before grant: want ErrForbidden, got %v", err)
	}

	// Grant the *parent* group 'owner' on the project; the subgroup member
	// inherits it through ancestor expansion.
	if err := svc.SetGroupProjectRole(ctx, founder, "acme", "dept", "acme", "owner"); err != nil {
		t.Fatalf("set group role: %v", err)
	}
	if err := svc.authorizeProjectMember(ctx, member, project.ID); err != nil {
		t.Fatalf("inherited member: %v", err)
	}
	if err := svc.authorizeProjectAdmin(ctx, member, project.ID); err != nil {
		t.Fatalf("inherited owner via ancestor group: %v", err)
	}

	// A stranger still has nothing.
	stranger := Actor{UserID: scopeMakeUser(t, st, "istranger")}
	if err := svc.authorizeProjectMember(ctx, stranger, project.ID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("stranger: want ErrForbidden, got %v", err)
	}

	// Revoke the grant: access is gone again.
	if err := svc.RemoveGroupProjectRole(ctx, founder, "acme", "dept", "acme"); err != nil {
		t.Fatalf("remove group role: %v", err)
	}
	if err := svc.authorizeProjectMember(ctx, member, project.ID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("after revoke: want ErrForbidden, got %v", err)
	}

	// Direct membership still works and 'member' is not admin.
	if err := st.AddProjectMember(ctx, db.AddProjectMemberParams{ProjectID: project.ID, UserID: memberID, Role: "member"}); err != nil {
		t.Fatalf("add project member: %v", err)
	}
	if err := svc.authorizeProjectMember(ctx, member, project.ID); err != nil {
		t.Fatalf("direct member: %v", err)
	}
	if err := svc.authorizeProjectAdmin(ctx, member, project.ID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("direct member as admin: want ErrForbidden, got %v", err)
	}
}

func TestBillingUsageAndPlan(t *testing.T) {
	svc, st := scopeTestService(t)
	ctx := context.Background()
	founderID := scopeMakeUser(t, st, "bfounder")
	founder := Actor{UserID: founderID}

	_, project, err := svc.CreateOrganization(ctx, founder, "acme2", "Acme2")
	if err != nil {
		t.Fatalf("create org: %v", err)
	}
	repo, err := st.CreateRepository(ctx, db.CreateRepositoryParams{
		ProjectID: project.ID, Slug: "web", Name: "web", Visibility: "private", DefaultBranch: "main",
	})
	if err != nil {
		t.Fatalf("create repo: %v", err)
	}
	pl, err := st.UpsertPipeline(ctx, db.UpsertPipelineParams{
		RepoID: repo.ID, WorkflowPath: ".github/workflows/ci.yml", Name: "ci",
	})
	if err != nil {
		t.Fatalf("upsert pipeline: %v", err)
	}
	now := time.Now().UTC()
	run, err := st.CreatePipelineRun(ctx, db.CreatePipelineRunParams{
		PipelineID: pl.ID, RunNumber: 1, Status: "running", Event: "push", Ref: "main", CommitSha: "abc123",
		StartedAt: pgtype.Timestamptz{Time: now.Add(-10 * time.Minute), Valid: true},
	})
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	if _, err := st.UpdatePipelineRunStatus(ctx, db.UpdatePipelineRunStatusParams{
		ID: run.ID, Status: "success", FinishedAt: pgtype.Timestamptz{Time: now, Valid: true},
	}); err != nil {
		t.Fatalf("finish run: %v", err)
	}

	// Tenant admin sees plan + usage. Organizations are not free: a new org
	// runs a Team trial (migration 000038) — no hard caps, not locked.
	view, err := svc.GetBilling(ctx, founder, "acme2")
	if err != nil {
		t.Fatalf("get billing: %v", err)
	}
	if view.Plan != PlanTrial || view.Locked || view.Limits != nil || view.TrialEndsAt == nil {
		t.Fatalf("new org: want an active trial without caps, got %+v", view)
	}
	// The free plan (still the individual tier) carries the hard limits.
	if _, err := svc.SetTenantPlan(ctx, Actor{IsAdmin: true}, "acme2", PlanFree); err != nil {
		t.Fatalf("set free plan: %v", err)
	}
	view, err = svc.GetBilling(ctx, founder, "acme2")
	if err != nil {
		t.Fatalf("get billing: %v", err)
	}
	if view.Plan != PlanFree {
		t.Fatalf("plan: want free, got %q", view.Plan)
	}
	if view.Usage.Repos != 1 {
		t.Fatalf("repos: want 1, got %d", view.Usage.Repos)
	}
	if view.Usage.PipelineMinutes < 9.9 || view.Usage.PipelineMinutes > 10.1 {
		t.Fatalf("pipeline minutes: want ~10, got %f", view.Usage.PipelineMinutes)
	}
	if view.Limits == nil || view.Limits.Repos == nil || *view.Limits.Repos != FreeMaxRepos {
		t.Fatalf("free plan should have hard limits, got %+v", view.Limits)
	}

	// A plain org member may not see billing.
	member := Actor{UserID: scopeMakeUser(t, st, "bmember")}
	if _, err := svc.GetBilling(ctx, member, "acme2"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("member billing: want ErrForbidden, got %v", err)
	}

	// Only a platform admin may change the plan.
	if _, err := svc.SetTenantPlan(ctx, founder, "acme2", PlanFree); !errors.Is(err, ErrForbidden) {
		t.Fatalf("org admin set plan: want ErrForbidden, got %v", err)
	}
	admin := Actor{UserID: founderID, IsAdmin: true}
	if _, err := svc.SetTenantPlan(ctx, admin, "acme2", "bogus"); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("invalid plan: want ErrInvalidInput, got %v", err)
	}
	if _, err := svc.SetTenantPlan(ctx, admin, "acme2", PlanInternal); err != nil {
		t.Fatalf("set plan internal: %v", err)
	}
	view2, err := svc.GetBilling(ctx, admin, "acme2")
	if err != nil {
		t.Fatalf("get billing after plan change: %v", err)
	}
	if view2.Plan != PlanInternal || view2.Limits != nil {
		t.Fatalf("internal plan should have no hard limits: got plan=%q limits=%+v", view2.Plan, view2.Limits)
	}
}

// TestGroupAccessAndOrgProjectsRead: the settings UI lists a group's project
// grants and the org's projects (for the picker); members can read both,
// outsiders can't.
func TestGroupAccessAndOrgProjectsRead(t *testing.T) {
	svc, st := scopeTestService(t)
	ctx := context.Background()
	admin := Actor{UserID: scopeMakeUser(t, st, "ga-admin")}
	if _, _, err := svc.CreateOrganization(ctx, admin, "gaorg", "GA Org"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateGroup(ctx, admin, "gaorg", "eng", "Engineering", ""); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetGroupProjectRole(ctx, admin, "gaorg", "eng", "gaorg", "owner"); err != nil {
		t.Fatal(err)
	}
	roles, err := svc.ListGroupProjectRoles(ctx, admin, "gaorg", "eng")
	if err != nil || len(roles) != 1 || roles[0].ProjectSlug != "gaorg" || roles[0].Role != "owner" {
		t.Fatalf("roles = %+v err=%v", roles, err)
	}
	projects, err := svc.ListOrgProjects(ctx, admin, "gaorg")
	if err != nil || len(projects) != 1 || projects[0].Slug != "gaorg" {
		t.Fatalf("projects = %+v err=%v", projects, err)
	}
	outsider := Actor{UserID: scopeMakeUser(t, st, "ga-outsider")}
	if _, err := svc.ListGroupProjectRoles(ctx, outsider, "gaorg", "eng"); !errors.Is(err, ErrForbidden) && !errors.Is(err, ErrNotFound) {
		t.Fatalf("outsider roles: %v", err)
	}
	if _, err := svc.ListOrgProjects(ctx, outsider, "gaorg"); !errors.Is(err, ErrForbidden) && !errors.Is(err, ErrNotFound) {
		t.Fatalf("outsider projects: %v", err)
	}
}

// TestGroupGrantCannotCrossTenants: an admin of org A must not be able to grant
// A's group a role on another org's project. SetGroupProjectRole looked the
// project up by slug alone, so an admin of ANY org (orgs are self-service)
// could make themselves owner of anyone's private project (found 2026-11-16).
func TestGroupGrantCannotCrossTenants(t *testing.T) {
	svc, st := scopeTestService(t)
	ctx := context.Background()
	adminA := Actor{UserID: scopeMakeUser(t, st, "xt-admin-a")}
	adminB := Actor{UserID: scopeMakeUser(t, st, "xt-admin-b")}
	insider := Actor{UserID: scopeMakeUser(t, st, "xt-member-a")}
	if _, _, err := svc.CreateOrganization(ctx, adminA, "xt-org-a", "Org A"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.CreateOrganization(ctx, adminB, "xt-org-b", "Org B"); err != nil {
		t.Fatal(err)
	}
	ta, _ := st.GetTenantBySlug(ctx, "xt-org-a")
	_ = st.AddTenantMember(ctx, db.AddTenantMemberParams{TenantID: ta.ID, UserID: insider.UserID, Role: "member"})
	if _, err := svc.CreateGroup(ctx, adminA, "xt-org-a", "crew", "Crew", ""); err != nil {
		t.Fatal(err)
	}
	if err := svc.AddGroupMember(ctx, adminA, "xt-org-a", "crew", insider.UserID); err != nil {
		t.Fatal(err)
	}
	projB, _ := st.GetProjectBySlug(ctx, "xt-org-b")

	// B's project: refused, indistinguishable from a missing project, and no access results.
	if err := svc.SetGroupProjectRole(ctx, adminA, "xt-org-a", "crew", "xt-org-b", "owner"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-tenant grant: want ErrNotFound, got %v", err)
	}
	if role, _ := svc.effectiveProjectRole(ctx, insider, projB.ID); role != "" {
		t.Fatalf("org A's member reaches org B's project as %q", role)
	}
	if err := svc.RemoveGroupProjectRole(ctx, adminA, "xt-org-a", "crew", "xt-org-b"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-tenant remove: want ErrNotFound, got %v", err)
	}
	// Another person's PERSONAL project is just as protected.
	victim := Actor{UserID: scopeMakeUser(t, st, "xt-victim")}
	vt, err := st.CreateTenant(ctx, db.CreateTenantParams{Slug: "xt-victim", Name: "xt-victim"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		// users.tenant_id references the tenant: detach before deleting it.
		_, _ = st.Pool().Exec(context.Background(), "UPDATE users SET tenant_id = NULL WHERE tenant_id = $1", vt.ID)
		_, _ = st.Pool().Exec(context.Background(), "DELETE FROM projects WHERE tenant_id = $1", vt.ID)
		_, _ = st.Pool().Exec(context.Background(), "DELETE FROM tenants WHERE id = $1", vt.ID)
	})
	if _, err := st.SetUserTenant(ctx, db.SetUserTenantParams{ID: victim.UserID, TenantID: uuid.NullUUID{UUID: vt.ID, Valid: true}}); err != nil {
		t.Fatal(err)
	}
	if err := svc.CreatePersonalProject(ctx, victim.UserID, "xt-victim"); err != nil {
		t.Fatalf("victim's personal project: %v", err)
	}
	if err := svc.SetGroupProjectRole(ctx, adminA, "xt-org-a", "crew", "xt-victim", "owner"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("grant on a personal project: want ErrNotFound, got %v", err)
	}

	// The legitimate path still works: A's own project.
	if err := svc.SetGroupProjectRole(ctx, adminA, "xt-org-a", "crew", "xt-org-a", "owner"); err != nil {
		t.Fatalf("same-org grant: %v", err)
	}
}
