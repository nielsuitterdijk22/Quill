package platform

import (
	"context"
	"errors"
	"testing"

	"github.com/nielsuitterdijk22/quill/internal/store/db"
)

// Project owners manage who reaches a project; org admins have no standing
// access but can claim ownership (audited by the HTTP layer).
func TestProjectAccessManagement(t *testing.T) {
	svc, st := scopeTestService(t)
	ctx := context.Background()
	owner := Actor{UserID: scopeMakeUser(t, st, "paowner")}
	tenant, project, err := svc.CreateOrganization(ctx, owner, "paorg", "PA Org")
	if err != nil {
		t.Fatal(err)
	}
	addMember := func(name, role string) Actor {
		id := scopeMakeUser(t, st, name)
		if err := st.AddTenantMember(ctx, db.AddTenantMemberParams{TenantID: tenant.ID, UserID: id, Role: role}); err != nil {
			t.Fatal(err)
		}
		return Actor{UserID: id}
	}
	bob := addMember("pabob", "member")
	admin := addMember("paadmin", "admin")
	outsider := Actor{UserID: scopeMakeUser(t, st, "paoutsider")}

	if _, err := svc.ListProjectAccess(ctx, bob, "paorg"); !errors.Is(err, ErrForbidden) {
		t.Errorf("member list = %v, want ErrForbidden", err)
	}
	if _, err := svc.ListProjectAccess(ctx, admin, "paorg"); !errors.Is(err, ErrForbidden) {
		t.Errorf("org admin without ownership list = %v, want ErrForbidden", err)
	}

	if err := svc.SetProjectUserRole(ctx, owner, "paorg", "pabob", "member"); err != nil {
		t.Fatal(err)
	}
	accessAll(t, svc, "bob granted", bob.UserID, project.ID, "member")
	if err := svc.SetProjectUserRole(ctx, owner, "paorg", "paoutsider", "member"); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("non-org-member grant = %v, want ErrInvalidInput", err)
	}
	if err := svc.SetProjectUserRole(ctx, owner, "paorg", "pabob", "god"); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("bad role = %v, want ErrInvalidInput", err)
	}
	if err := svc.SetProjectUserRole(ctx, outsider, "paorg", "pabob", "owner"); !errors.Is(err, ErrForbidden) {
		t.Errorf("outsider grant = %v, want ErrForbidden", err)
	}

	if _, err := svc.CreateGroup(ctx, admin, "paorg", "viewers", "Viewers"); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetProjectGroupRole(ctx, owner, "paorg", "viewers", "read"); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetGroupProjectRole(ctx, admin, "paorg", "viewers", "paorg", "owner"); !errors.Is(err, ErrForbidden) {
		t.Errorf("org admin granting a group on a project they don't own = %v, want ErrForbidden", err)
	}

	if err := svc.SetProjectUserRole(ctx, owner, "paorg", "paowner", "member"); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("demoting the only owner = %v, want ErrInvalidInput", err)
	}
	if err := svc.RemoveProjectUser(ctx, owner, "paorg", owner.UserID); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("removing the only owner = %v, want ErrInvalidInput", err)
	}

	if err := svc.SelfGrantProjectOwner(ctx, bob, "paorg"); !errors.Is(err, ErrForbidden) {
		t.Errorf("member claim = %v, want ErrForbidden", err)
	}
	if err := svc.SelfGrantProjectOwner(ctx, admin, "paorg"); err != nil {
		t.Fatal(err)
	}
	accessAll(t, svc, "admin after claim", admin.UserID, project.ID, "owner")
	if err := svc.SetProjectUserRole(ctx, admin, "paorg", "paowner", "member"); err != nil {
		t.Errorf("demote with another owner present: %v", err)
	}

	access, err := svc.ListProjectAccess(ctx, admin, "paorg")
	if err != nil || len(access.Users) != 3 || len(access.Groups) != 1 {
		t.Fatalf("access = %+v (err %v), want 3 users and 1 group", access, err)
	}
	if err := svc.RemoveProjectUser(ctx, admin, "paorg", bob.UserID); err != nil {
		t.Fatal(err)
	}
	accessAll(t, svc, "bob revoked", bob.UserID, project.ID, "")
}

func TestPersonalProjectsAreNotShareable(t *testing.T) {
	svc, st := scopeTestService(t)
	ctx := context.Background()
	u := scopeMakeUser(t, st, "papersonal")
	if err := svc.CreatePersonalProject(ctx, u, "papersonal"); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetProjectUserRole(ctx, Actor{UserID: u}, "papersonal", "papersonal", "member"); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("share personal = %v, want ErrInvalidInput", err)
	}
}
