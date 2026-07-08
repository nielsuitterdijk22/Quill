package platform

import (
	"context"
	"errors"
	"testing"

	"github.com/nielsuitterdijk22/quill/internal/store/db"
)

// These white-box integration tests cover organization creation (an org-kind
// tenant with the creator as admin and a seeded first project) and the tenant
// admin authorization that gates org-wide governance. They reuse the shared
// scopeTestService/scopeMakeUser helpers and are gated on QUILL_TEST_DATABASE_URL.

func TestCreateOrganization(t *testing.T) {
	svc, st := scopeTestService(t)
	ctx := context.Background()
	founderID := scopeMakeUser(t, st, "founder")
	founder := Actor{UserID: founderID}

	tenant, project, err := svc.CreateOrganization(ctx, founder, "acme", "Acme Inc")
	if err != nil {
		t.Fatalf("create organization: %v", err)
	}
	if tenant.Slug != "acme" || tenant.Name != "Acme Inc" {
		t.Fatalf("tenant fields: %+v", tenant)
	}
	if project.Slug != "acme" || project.IsPersonal {
		t.Fatalf("org project should be a non-personal 'acme' project, got %+v", project)
	}

	// The founder is the org admin and a member; a stranger is neither.
	if err := svc.authorizeTenantAdmin(ctx, founder, tenant.ID); err != nil {
		t.Fatalf("founder should be tenant admin: %v", err)
	}
	if err := svc.authorizeTenantMember(ctx, founder, tenant.ID); err != nil {
		t.Fatalf("founder should be tenant member: %v", err)
	}
	stranger := Actor{UserID: scopeMakeUser(t, st, "stranger")}
	if err := svc.authorizeTenantAdmin(ctx, stranger, tenant.ID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("stranger tenant admin: want ErrForbidden, got %v", err)
	}
	if err := svc.authorizeTenantMember(ctx, stranger, tenant.ID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("stranger tenant member: want ErrForbidden, got %v", err)
	}

	// The org surfaces in the founder's org list with the admin role.
	orgs, err := svc.ListOrganizations(ctx, founder)
	if err != nil {
		t.Fatalf("list organizations: %v", err)
	}
	if len(orgs) != 1 || orgs[0].Slug != "acme" || orgs[0].Role != "admin" {
		t.Fatalf("want one admin org 'acme', got %+v", orgs)
	}

	// A duplicate org slug conflicts.
	if _, _, err := svc.CreateOrganization(ctx, founder, "acme", "Acme Again"); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate org: want ErrConflict, got %v", err)
	}
}

func TestOrgInvitesAndMembers(t *testing.T) {
	svc, st := scopeTestService(t)
	ctx := context.Background()
	founder := Actor{UserID: scopeMakeUser(t, st, "founder")}
	if _, _, err := svc.CreateOrganization(ctx, founder, "acme", "Acme Inc"); err != nil {
		t.Fatalf("create org: %v", err)
	}

	// The last admin cannot be demoted.
	if err := svc.UpdateOrgMemberRole(ctx, founder, "acme", founder.UserID, "member"); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("demote last admin: want ErrInvalidInput, got %v", err)
	}

	// Admin invites a member; the raw token builds the accept link.
	res, err := svc.CreateInvite(ctx, founder, "acme", "jane@acme.com", "member")
	if err != nil {
		t.Fatalf("create invite: %v", err)
	}
	if res.Token == "" || res.Invite.Email != "jane@acme.com" || res.Invite.Role != "member" {
		t.Fatalf("invite result: %+v", res)
	}

	// A second invite to the same email revokes the first, leaving one pending.
	res2, err := svc.CreateInvite(ctx, founder, "acme", "jane@acme.com", "admin")
	if err != nil {
		t.Fatalf("re-invite: %v", err)
	}
	pending, err := svc.ListInvites(ctx, founder, "acme")
	if err != nil {
		t.Fatalf("list invites: %v", err)
	}
	if len(pending) != 1 || pending[0].Role != "admin" {
		t.Fatalf("want one pending admin invite, got %+v", pending)
	}

	// The stale first token no longer accepts; the current one does.
	jane := Actor{UserID: scopeMakeUser(t, st, "jane")}
	if _, err := svc.AcceptInvite(ctx, jane, res.Token); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("stale token accept: want ErrInvalidInput, got %v", err)
	}
	slug, err := svc.AcceptInvite(ctx, jane, res2.Token)
	if err != nil {
		t.Fatalf("accept invite: %v", err)
	}
	if slug != "acme" {
		t.Fatalf("accept returned slug %q", slug)
	}

	// Jane is now an admin member; the roster shows both.
	members, err := svc.ListOrgMembers(ctx, founder, "acme")
	if err != nil {
		t.Fatalf("list members: %v", err)
	}
	if len(members) != 2 {
		t.Fatalf("want 2 members, got %+v", members)
	}

	// A plain member cannot invite. Demote jane, then she is forbidden.
	if err := svc.UpdateOrgMemberRole(ctx, founder, "acme", jane.UserID, "member"); err != nil {
		t.Fatalf("demote jane: %v", err)
	}
	if _, err := svc.CreateInvite(ctx, jane, "acme", "x@acme.com", "member"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("member invite: want ErrForbidden, got %v", err)
	}

	// Removing a plain member works; removing the last admin (founder) does not.
	if err := svc.RemoveOrgMember(ctx, founder, "acme", jane.UserID); err != nil {
		t.Fatalf("remove member: %v", err)
	}
	if err := svc.RemoveOrgMember(ctx, founder, "acme", founder.UserID); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("remove last admin: want ErrInvalidInput, got %v", err)
	}
}

func TestOrgAdminManagesTenantPolicies(t *testing.T) {
	svc, st := scopeTestService(t)
	ctx := context.Background()
	founder := Actor{UserID: scopeMakeUser(t, st, "founder")}
	tenant, _, err := svc.CreateOrganization(ctx, founder, "acme", "Acme Inc")
	if err != nil {
		t.Fatalf("create organization: %v", err)
	}

	// An org admin may set org-wide (tenant-scoped) branch policies — previously
	// reserved for platform admins.
	if _, err := svc.SetTenantBranchPolicy(ctx, founder, "acme", BranchPolicyInput{Pattern: "main", RequiredApprovals: 2}); err != nil {
		t.Fatalf("org admin tenant branch policy: %v", err)
	}
	if _, err := svc.SetTenantEnvironmentPolicy(ctx, founder, "acme", EnvironmentPolicyInput{Selector: "production", RequiredApprovals: 1}); err != nil {
		t.Fatalf("org admin tenant env policy: %v", err)
	}

	// A non-member cannot.
	stranger := Actor{UserID: scopeMakeUser(t, st, "stranger")}
	if _, err := svc.SetTenantBranchPolicy(ctx, stranger, "acme", BranchPolicyInput{Pattern: "main"}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("stranger tenant write: want ErrForbidden, got %v", err)
	}

	// A plain org member (non-admin) may read but not write.
	memberID := scopeMakeUser(t, st, "member")
	if err := svc.store.AddTenantMember(ctx, db.AddTenantMemberParams{TenantID: tenant.ID, UserID: memberID, Role: "member"}); err != nil {
		t.Fatalf("add member: %v", err)
	}
	member := Actor{UserID: memberID}
	if _, _, err := svc.ListTenantBranchPolicies(ctx, member, "acme"); err != nil {
		t.Fatalf("member read tenant policies: %v", err)
	}
	if _, err := svc.SetTenantBranchPolicy(ctx, member, "acme", BranchPolicyInput{Pattern: "dev"}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("member tenant write: want ErrForbidden, got %v", err)
	}
}
