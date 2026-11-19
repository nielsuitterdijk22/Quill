package platform

import (
	"context"
	"errors"
	"testing"

	"github.com/nielsuitterdijk22/quill/internal/store/db"
)

// An IdP group adopts a same-named manual group: the id, slug and project
// grants stay, the roster is overwritten by the IdP, and from then on the
// group cannot be edited by hand.
func TestIdPGroupAdoptsManualGroup(t *testing.T) {
	svc, st := scopeTestService(t)
	ctx := context.Background()
	founder := Actor{UserID: scopeMakeUser(t, st, "idpfounder")}
	tenant, project, err := svc.CreateOrganization(ctx, founder, "idporg", "IdP Org")
	if err != nil {
		t.Fatal(err)
	}
	old := scopeMakeUser(t, st, "idpold")
	if err := st.AddTenantMember(ctx, db.AddTenantMemberParams{TenantID: tenant.ID, UserID: old, Role: "member"}); err != nil {
		t.Fatal(err)
	}
	manual, err := svc.CreateGroup(ctx, founder, "idporg", "platform", "Platform")
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.AddGroupMember(ctx, founder, "idporg", "platform", old); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetProjectGroupRole(ctx, founder, "idporg", "platform", "member"); err != nil {
		t.Fatal(err)
	}
	accessAll(t, svc, "before adoption", old, project.ID, "member")

	view, err := svc.SCIMCreateGroup(ctx, tenant, "Platform", "ext-1", nil)
	if err != nil {
		t.Fatalf("scim create: %v", err)
	}
	if view.GroupID != manual.ID {
		t.Fatalf("IdP group got a new group %v, want the adopted %v", view.GroupID, manual.ID)
	}
	groups, err := svc.ListGroups(ctx, founder, "idporg")
	if err != nil || len(groups) != 1 || groups[0].Source != "scim" || groups[0].Slug != "platform" {
		t.Fatalf("groups = %+v (err %v), want one scim group with slug platform", groups, err)
	}
	// Grants survive, the old hand-made roster does not.
	if n, _ := st.GetGroupProjectRoleValue(ctx, manual.ID, project.ID); n != "member" {
		t.Errorf("grant after adoption = %q, want member", n)
	}
	accessAll(t, svc, "dropped by the IdP roster", old, project.ID, "")

	// Hand edits are refused.
	if err := svc.AddGroupMember(ctx, founder, "idporg", "platform", old); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("manual add to IdP group = %v, want ErrInvalidInput", err)
	}
	if err := svc.DeleteGroup(ctx, founder, "idporg", "platform"); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("manual delete of IdP group = %v, want ErrInvalidInput", err)
	}
	if _, err := svc.UpdateGroup(ctx, founder, "idporg", "platform", "Renamed", "platform"); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("manual rename of IdP group = %v, want ErrInvalidInput", err)
	}
}
