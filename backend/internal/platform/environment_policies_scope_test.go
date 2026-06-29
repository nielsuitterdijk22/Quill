package platform

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

// These white-box integration tests cover environment-policy scoping across
// tenant, project, and repo, plus how inheritance and locking surface in the
// per-scope reads. They reuse the shared scopeTestService/seedScopeRepo helpers
// (policies_scope_test.go) and are gated on QUILL_TEST_DATABASE_URL so CI
// without a database stays green. Pure rule semantics live in internal/policy;
// here we verify the platform wiring (authz, scope addressing, CRUD, and
// inherited reads).

func TestTenantEnvironmentPolicyRequiresPlatformAdmin(t *testing.T) {
	svc, st := scopeTestService(t)
	ctx := context.Background()
	owner, _ := seedScopeRepo(t, svc, st, "acme", "widget")

	// A project owner is not a platform admin, so tenant writes are forbidden.
	_, err := svc.SetTenantEnvironmentPolicy(ctx, owner, "default", EnvironmentPolicyInput{Selector: "production", RequiredApprovals: 2})
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("non-admin tenant write: want ErrForbidden, got %v", err)
	}

	admin := Actor{UserID: owner.UserID, IsAdmin: true}
	if _, err := svc.SetTenantEnvironmentPolicy(ctx, admin, "default", EnvironmentPolicyInput{Selector: "production", RequiredApprovals: 2, Locked: true}); err != nil {
		t.Fatalf("admin tenant write: %v", err)
	}

	// Reads are open to any member of the tenant (the gates govern their
	// deploys), but closed to users outside it.
	tenant, err := st.GetTenantBySlug(ctx, "default")
	if err != nil {
		t.Fatalf("get default tenant: %v", err)
	}
	member := Actor{UserID: owner.UserID, TenantID: tenant.ID}
	if _, _, err := svc.ListTenantEnvironmentPolicies(ctx, member, "default"); err != nil {
		t.Fatalf("tenant member read: %v", err)
	}
	stranger := Actor{UserID: scopeMakeUser(t, st, "env-stranger"), TenantID: uuid.New()}
	if _, _, err := svc.ListTenantEnvironmentPolicies(ctx, stranger, "default"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("non-member tenant read: want ErrForbidden, got %v", err)
	}
}

func TestProjectEnvironmentPolicyAuthz(t *testing.T) {
	svc, st := scopeTestService(t)
	ctx := context.Background()
	owner, _ := seedScopeRepo(t, svc, st, "acme", "widget")

	// Owners may write project environment policies.
	if _, err := svc.SetProjectEnvironmentPolicy(ctx, owner, "acme", EnvironmentPolicyInput{Selector: "staging", RequiredApprovals: 1}); err != nil {
		t.Fatalf("owner project write: %v", err)
	}

	// A non-member may neither read nor write project environment policies.
	stranger := Actor{UserID: scopeMakeUser(t, st, "env-stranger")}
	if _, _, err := svc.ListProjectEnvironmentPolicies(ctx, stranger, "acme"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("stranger project read: want ErrForbidden, got %v", err)
	}
	if _, err := svc.SetProjectEnvironmentPolicy(ctx, stranger, "acme", EnvironmentPolicyInput{Selector: "staging"}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("stranger project write: want ErrForbidden, got %v", err)
	}
}

func TestRepoListSurfacesInheritedEnvironmentPolicies(t *testing.T) {
	svc, st := scopeTestService(t)
	ctx := context.Background()
	owner, _ := seedScopeRepo(t, svc, st, "acme", "widget")
	admin := Actor{UserID: owner.UserID, IsAdmin: true}

	if _, err := svc.SetTenantEnvironmentPolicy(ctx, admin, "default", EnvironmentPolicyInput{Selector: "production", RequiredApprovals: 2, Locked: true}); err != nil {
		t.Fatalf("tenant policy: %v", err)
	}
	if _, err := svc.SetProjectEnvironmentPolicy(ctx, owner, "acme", EnvironmentPolicyInput{Selector: "staging", RequiredApprovals: 1}); err != nil {
		t.Fatalf("project policy: %v", err)
	}
	if _, err := svc.SetEnvironmentPolicy(ctx, owner, "acme", "widget", EnvironmentPolicyInput{
		Selector:                   "production",
		RequiredApprovals:          1,
		AllowedSourceBranches:      []string{"main", "release/*"},
		RequirePreviousEnvironment: "staging",
		RequireSuccessfulRun:       true,
		MinWaitMinutes:             30,
	}); err != nil {
		t.Fatalf("repo policy: %v", err)
	}

	repo, set, err := svc.ListEnvironmentPolicies(ctx, owner, "acme", "widget")
	if err != nil {
		t.Fatalf("list repo env policies: %v", err)
	}
	_ = repo
	if len(set.Own) != 1 || set.Own[0].Scope != "repo" {
		t.Fatalf("want one repo-scoped own policy, got %+v", set.Own)
	}
	// Round-trip of the full rule body.
	got := set.Own[0].Rule
	if got.RequiredApprovals != 1 || got.RequirePreviousEnvironment != "staging" ||
		!got.RequireSuccessfulRun || got.MinWaitMinutes != 30 ||
		len(got.AllowedSourceBranches) != 2 {
		t.Fatalf("repo rule did not round-trip: %+v", got)
	}
	if len(set.Inherited) != 2 {
		t.Fatalf("want two inherited policies (tenant+project), got %+v", set.Inherited)
	}
	// Inheritance is ordered broad -> narrow: tenant first, then project.
	if set.Inherited[0].Scope != "tenant" || set.Inherited[1].Scope != "project" {
		t.Fatalf("inherited order wrong: %+v", set.Inherited)
	}
	if !set.Inherited[0].Locked {
		t.Fatalf("tenant policy should report locked")
	}
}

func TestEnvironmentPolicyRepoScopeUnlocked(t *testing.T) {
	svc, st := scopeTestService(t)
	ctx := context.Background()
	owner, _ := seedScopeRepo(t, svc, st, "acme", "widget")

	// A repo is the narrowest scope, so locking is forced false even if asked.
	view, err := svc.SetEnvironmentPolicy(ctx, owner, "acme", "widget", EnvironmentPolicyInput{Selector: "production", Locked: true})
	if err != nil {
		t.Fatalf("repo env write: %v", err)
	}
	if view.Locked {
		t.Fatalf("repo-scope environment policy must not be lockable")
	}
}

func TestEnvironmentPolicyUpsertAndDelete(t *testing.T) {
	svc, st := scopeTestService(t)
	ctx := context.Background()
	owner, _ := seedScopeRepo(t, svc, st, "acme", "widget")

	if _, err := svc.SetEnvironmentPolicy(ctx, owner, "acme", "widget", EnvironmentPolicyInput{Selector: "production", RequiredApprovals: 1}); err != nil {
		t.Fatalf("create: %v", err)
	}
	// Upsert the same selector — should update in place, not add a second row.
	if _, err := svc.SetEnvironmentPolicy(ctx, owner, "acme", "widget", EnvironmentPolicyInput{Selector: "production", RequiredApprovals: 3}); err != nil {
		t.Fatalf("update: %v", err)
	}
	_, set, err := svc.ListEnvironmentPolicies(ctx, owner, "acme", "widget")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(set.Own) != 1 || set.Own[0].Rule.RequiredApprovals != 3 {
		t.Fatalf("upsert did not replace in place: %+v", set.Own)
	}

	if err := svc.DeleteEnvironmentPolicy(ctx, owner, "acme", "widget", "production"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := svc.DeleteEnvironmentPolicy(ctx, owner, "acme", "widget", "production"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("delete missing: want ErrNotFound, got %v", err)
	}
}
