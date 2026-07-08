package platform

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/google/uuid"

	"github.com/nielsuitterdijk22/quill/internal/config"
	"github.com/nielsuitterdijk22/quill/internal/forgejo"
	"github.com/nielsuitterdijk22/quill/internal/store"
	"github.com/nielsuitterdijk22/quill/internal/store/db"
)

// These white-box integration tests cover branch-policy scoping across tenant,
// project, and repo, plus how inheritance and locking flow through the merge
// gate's effective-rule resolver. They are gated on QUILL_TEST_DATABASE_URL so
// CI without a database stays green. Pure resolver semantics live in
// internal/policy; here we verify the platform wiring (authz, scope addressing,
// and the gate loading all three scopes).

func scopeTestService(t *testing.T) (*Service, *store.Store) {
	t.Helper()
	dsn := os.Getenv("QUILL_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("QUILL_TEST_DATABASE_URL not set; skipping policy scope integration test")
	}
	if err := store.Migrate(dsn); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	st, err := store.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(st.Close)
	reset := func() {
		// Leave the seeded default tenant; clear everything beneath it. Truncating
		// projects/users first frees the org tenants (projects reference tenants with
		// ON DELETE RESTRICT), so the org-tenant delete afterward never trips it.
		_, _ = st.Pool().Exec(context.Background(), "TRUNCATE projects, users CASCADE")
		_, _ = st.Pool().Exec(context.Background(), "DELETE FROM policies")
		_, _ = st.Pool().Exec(context.Background(), "DELETE FROM tenants WHERE kind = 'org'")
	}
	reset()
	t.Cleanup(reset)
	svc := NewService(st, forgejo.New(config.ForgejoConfig{}), nil)
	return svc, st
}

func scopeMakeUser(t *testing.T, st *store.Store, username string) uuid.UUID {
	t.Helper()
	u, err := st.CreateUser(context.Background(), db.CreateUserParams{
		Username:    username,
		Email:       username + "@example.test",
		DisplayName: username,
		IsActive:    true,
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	return u.ID
}

// seedScopeRepo creates a project + repo owned by a fresh user and returns the
// owner actor and the stored repository.
func seedScopeRepo(t *testing.T, svc *Service, st *store.Store, projectSlug, repoSlug string) (Actor, db.Repository) {
	t.Helper()
	ctx := context.Background()
	ownerID := scopeMakeUser(t, st, "owner-"+projectSlug+"-"+repoSlug)
	project, err := svc.CreateProject(ctx, Actor{UserID: ownerID}, CreateProjectInput{Slug: projectSlug, Name: projectSlug})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	if _, err := svc.CreateRepo(ctx, Actor{UserID: ownerID}, projectSlug, CreateRepoInput{Slug: repoSlug, Name: repoSlug}); err != nil {
		t.Fatalf("create repo: %v", err)
	}
	repo, err := st.GetRepositoryBySlug(ctx, db.GetRepositoryBySlugParams{ProjectID: project.ID, Lower: repoSlug})
	if err != nil {
		t.Fatalf("get repo: %v", err)
	}
	return Actor{UserID: ownerID}, repo
}

func TestTenantPolicyRequiresPlatformAdmin(t *testing.T) {
	svc, st := scopeTestService(t)
	ctx := context.Background()
	owner, _ := seedScopeRepo(t, svc, st, "acme", "widget")

	// A project owner is not a platform admin, so tenant writes are forbidden.
	_, err := svc.SetTenantBranchPolicy(ctx, owner, "default", BranchPolicyInput{Pattern: "main", RequiredApprovals: 2})
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("non-admin tenant write: want ErrForbidden, got %v", err)
	}

	admin := Actor{UserID: owner.UserID, IsAdmin: true}
	if _, err := svc.SetTenantBranchPolicy(ctx, admin, "default", BranchPolicyInput{Pattern: "main", RequiredApprovals: 2, Locked: true}); err != nil {
		t.Fatalf("admin tenant write: %v", err)
	}

	// Tenant policies govern everyone's work, so any member of the tenant may
	// read them — even a non-admin — while a user outside the tenant may not.
	tenant, err := st.GetTenantBySlug(ctx, "default")
	if err != nil {
		t.Fatalf("get default tenant: %v", err)
	}
	member := Actor{UserID: owner.UserID, TenantID: tenant.ID}
	if _, _, err := svc.ListTenantBranchPolicies(ctx, member, "default"); err != nil {
		t.Fatalf("tenant member read: %v", err)
	}
	stranger := Actor{UserID: scopeMakeUser(t, st, "stranger"), TenantID: uuid.New()}
	if _, _, err := svc.ListTenantBranchPolicies(ctx, stranger, "default"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("non-member tenant read: want ErrForbidden, got %v", err)
	}
}

func TestProjectPolicyAuthz(t *testing.T) {
	svc, st := scopeTestService(t)
	ctx := context.Background()
	owner, _ := seedScopeRepo(t, svc, st, "acme", "widget")

	// Owners may write project policies.
	if _, err := svc.SetProjectBranchPolicy(ctx, owner, "acme", BranchPolicyInput{Pattern: "main", RequiredApprovals: 1}); err != nil {
		t.Fatalf("owner project write: %v", err)
	}

	// A non-member may neither read nor write project policies.
	stranger := Actor{UserID: scopeMakeUser(t, st, "stranger")}
	if _, _, err := svc.ListProjectBranchPolicies(ctx, stranger, "acme"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("stranger project read: want ErrForbidden, got %v", err)
	}
	if _, err := svc.SetProjectBranchPolicy(ctx, stranger, "acme", BranchPolicyInput{Pattern: "main"}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("stranger project write: want ErrForbidden, got %v", err)
	}
}

func TestRepoListSurfacesInheritedPolicies(t *testing.T) {
	svc, st := scopeTestService(t)
	ctx := context.Background()
	owner, _ := seedScopeRepo(t, svc, st, "acme", "widget")
	admin := Actor{UserID: owner.UserID, IsAdmin: true}

	if _, err := svc.SetTenantBranchPolicy(ctx, admin, "default", BranchPolicyInput{Pattern: "main", RequiredApprovals: 2, Locked: true}); err != nil {
		t.Fatalf("tenant policy: %v", err)
	}
	if _, err := svc.SetProjectBranchPolicy(ctx, owner, "acme", BranchPolicyInput{Pattern: "release/*", RequiredApprovals: 1}); err != nil {
		t.Fatalf("project policy: %v", err)
	}
	if _, err := svc.SetBranchPolicy(ctx, owner, "acme", "widget", BranchPolicyInput{Pattern: "main", RequiredApprovals: 1}); err != nil {
		t.Fatalf("repo policy: %v", err)
	}

	_, set, err := svc.ListBranchPolicies(ctx, owner, "acme", "widget")
	if err != nil {
		t.Fatalf("list repo policies: %v", err)
	}
	if len(set.Own) != 1 || set.Own[0].Scope != "repo" {
		t.Fatalf("want one repo-scoped own policy, got %+v", set.Own)
	}
	if len(set.Inherited) != 2 {
		t.Fatalf("want two inherited policies (tenant+project), got %+v", set.Inherited)
	}
	// Inheritance is ordered broad -> narrow: tenant first, then project.
	if set.Inherited[0].Scope != "tenant" || set.Inherited[1].Scope != "project" {
		t.Fatalf("inherited order wrong: %+v", set.Inherited)
	}
}

func TestEffectiveGateHonoursLockedInheritance(t *testing.T) {
	svc, st := scopeTestService(t)
	ctx := context.Background()
	owner, repo := seedScopeRepo(t, svc, st, "acme", "widget")
	admin := Actor{UserID: owner.UserID, IsAdmin: true}

	// Tenant locks a 2-approval floor on main; the repo tries to weaken it to 1.
	if _, err := svc.SetTenantBranchPolicy(ctx, admin, "default", BranchPolicyInput{Pattern: "main", RequiredApprovals: 2, RequirePullRequest: true, Locked: true}); err != nil {
		t.Fatalf("tenant policy: %v", err)
	}
	if _, err := svc.SetBranchPolicy(ctx, owner, "acme", "widget", BranchPolicyInput{Pattern: "main", RequiredApprovals: 1}); err != nil {
		t.Fatalf("repo policy: %v", err)
	}

	// Unanimous-allow composition: the strictest applicable scope governs, so the
	// tenant's 2-approval floor holds even though the repo declared 1.
	state, err := svc.branchGate(ctx, repo, pullOnto("main", "feature", "author"), nil)
	if err != nil {
		t.Fatalf("branch gate: %v", err)
	}
	if !state.Applies {
		t.Fatalf("expected an applicable gate for main")
	}
	if state.RequiredApprovals != 2 {
		t.Fatalf("tenant floor not honoured: approvals=%d want 2", state.RequiredApprovals)
	}
	if !state.Blocked {
		t.Fatalf("expected block with 0 of 2 approvals, got %+v", state)
	}
	if state.Pattern != "main" {
		t.Fatalf("pattern=%q want main", state.Pattern)
	}
}

func TestEffectiveGateAllowsTighteningUnlocked(t *testing.T) {
	svc, st := scopeTestService(t)
	ctx := context.Background()
	owner, repo := seedScopeRepo(t, svc, st, "acme", "widget")
	admin := Actor{UserID: owner.UserID, IsAdmin: true}

	// Unlocked tenant baseline of 1; repo tightens to 3. Strictest scope wins.
	if _, err := svc.SetTenantBranchPolicy(ctx, admin, "default", BranchPolicyInput{Pattern: "main", RequiredApprovals: 1}); err != nil {
		t.Fatalf("tenant policy: %v", err)
	}
	if _, err := svc.SetBranchPolicy(ctx, owner, "acme", "widget", BranchPolicyInput{Pattern: "main", RequiredApprovals: 3}); err != nil {
		t.Fatalf("repo policy: %v", err)
	}

	state, err := svc.branchGate(ctx, repo, pullOnto("main", "feature", "author"), nil)
	if err != nil {
		t.Fatalf("branch gate: %v", err)
	}
	if state.RequiredApprovals != 3 {
		t.Fatalf("repo tightening not applied: required=%d want 3", state.RequiredApprovals)
	}
}
