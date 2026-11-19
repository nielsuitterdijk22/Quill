package platform

import (
	"context"
	"errors"
	"testing"

	"github.com/nielsuitterdijk22/quill/internal/store/db"
)

// Explicit access: org membership never implies project access, the read role
// can look but not touch, and an "internal" repository is readable by the whole
// organization without ever becoming writable by it.

func TestReadRoleCanReadButNotWrite(t *testing.T) {
	svc, st := scopeTestService(t)
	ctx := context.Background()
	founder := Actor{UserID: scopeMakeUser(t, st, "xfounder")}
	tenant, project, err := svc.CreateOrganization(ctx, founder, "xorg", "X Org")
	if err != nil {
		t.Fatal(err)
	}
	reader := Actor{UserID: scopeMakeUser(t, st, "xreader")}
	if err := st.AddTenantMember(ctx, db.AddTenantMemberParams{TenantID: tenant.ID, UserID: reader.UserID, Role: "member"}); err != nil {
		t.Fatal(err)
	}

	// Via a group grant of the read role.
	if _, err := svc.CreateGroup(ctx, founder, "xorg", "viewers", "Viewers"); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetGroupProjectRole(ctx, founder, "xorg", "viewers", "xorg", "read"); err != nil {
		t.Fatalf("grant read to a group: %v", err)
	}
	if err := svc.AddGroupMember(ctx, founder, "xorg", "viewers", reader.UserID); err != nil {
		t.Fatal(err)
	}

	if err := svc.authorizeProjectRead(ctx, reader, project.ID); err != nil {
		t.Errorf("read role: read gate = %v, want nil", err)
	}
	if err := svc.authorizeProjectMember(ctx, reader, project.ID); !errors.Is(err, ErrForbidden) {
		t.Errorf("read role: write gate = %v, want ErrForbidden", err)
	}
	if err := svc.authorizeProjectAdmin(ctx, reader, project.ID); !errors.Is(err, ErrForbidden) {
		t.Errorf("read role: admin gate = %v, want ErrForbidden", err)
	}
	if _, err := svc.CreateRepo(ctx, reader, "xorg", CreateRepoInput{Slug: "nope", Name: "nope"}); !errors.Is(err, ErrForbidden) {
		t.Errorf("read role creating a repo = %v, want ErrForbidden", err)
	}
	if _, err := svc.GetProject(ctx, reader, "xorg"); err != nil {
		t.Errorf("read role GetProject = %v, want nil", err)
	}
	if err := svc.authorizeProjectMember(ctx, founder, project.ID); err != nil {
		t.Errorf("owner write gate = %v", err)
	}
}

func TestInternalRepoReadableByOrgMembersOnly(t *testing.T) {
	svc, st := scopeTestService(t)
	ctx := context.Background()
	founder := Actor{UserID: scopeMakeUser(t, st, "ifounder")}
	tenant, project, err := svc.CreateOrganization(ctx, founder, "iorg", "I Org")
	if err != nil {
		t.Fatal(err)
	}
	colleague := Actor{UserID: scopeMakeUser(t, st, "icolleague")}
	if err := st.AddTenantMember(ctx, db.AddTenantMemberParams{TenantID: tenant.ID, UserID: colleague.UserID, Role: "member"}); err != nil {
		t.Fatal(err)
	}
	outsider := Actor{UserID: scopeMakeUser(t, st, "ioutsider")}

	internal := db.Repository{Visibility: VisibilityInternal}
	private := db.Repository{Visibility: VisibilityPrivate}

	if err := svc.authorizeRepoRead(ctx, colleague, project, internal); err != nil {
		t.Errorf("org member reading internal repo = %v, want nil", err)
	}
	if err := svc.authorizeRepoRead(ctx, colleague, project, private); !errors.Is(err, ErrForbidden) {
		t.Errorf("org member reading private repo without a grant = %v, want ErrForbidden", err)
	}
	if err := svc.authorizeRepoRead(ctx, outsider, project, internal); !errors.Is(err, ErrForbidden) {
		t.Errorf("non-member reading internal repo = %v, want ErrForbidden", err)
	}
	if err := svc.authorizeRepoRead(ctx, Actor{}, project, internal); !errors.Is(err, ErrForbidden) {
		t.Errorf("anonymous reading internal repo = %v, want ErrForbidden", err)
	}
	// Internal never means writable.
	if err := svc.authorizeProjectMember(ctx, colleague, project.ID); !errors.Is(err, ErrForbidden) {
		t.Errorf("org member write gate on internal repo's project = %v, want ErrForbidden", err)
	}
}
