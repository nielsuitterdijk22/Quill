package platform

import (
	"context"
	"errors"
	"testing"
	"time"

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

	// Tenant admin sees plan + usage; internal plan has no hard limits.
	view, err := svc.GetBilling(ctx, founder, "acme2")
	if err != nil {
		t.Fatalf("get billing: %v", err)
	}
	if view.Plan != PlanInternal {
		t.Fatalf("plan: want internal, got %q", view.Plan)
	}
	if view.Usage.Repos != 1 {
		t.Fatalf("repos: want 1, got %d", view.Usage.Repos)
	}
	if view.Usage.PipelineMinutes < 9.9 || view.Usage.PipelineMinutes > 10.1 {
		t.Fatalf("pipeline minutes: want ~10, got %f", view.Usage.PipelineMinutes)
	}
	if view.Limits != nil {
		t.Fatalf("internal plan should have no hard limits, got %+v", view.Limits)
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
	if _, err := svc.SetTenantPlan(ctx, admin, "acme2", PlanFree); err != nil {
		t.Fatalf("set plan free: %v", err)
	}
	view2, err := svc.GetBilling(ctx, admin, "acme2")
	if err != nil {
		t.Fatalf("get billing after plan change: %v", err)
	}
	if view2.Plan != PlanFree || view2.Limits == nil || view2.Limits.Repos == nil || *view2.Limits.Repos != FreeMaxRepos {
		t.Fatalf("free plan limits: got plan=%q limits=%+v", view2.Plan, view2.Limits)
	}
}
