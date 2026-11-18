package platform

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/nielsuitterdijk22/quill/internal/store/db"
)

// Access resolver tests (DB-gated like the other scope tests).
//
// One SQL definition (project_access) decides who can reach a project; the
// authorization checks, the project switcher and the cross-cutting user views
// must all agree with it. The invariant under test: in an organization, a
// grant — direct, via a group, or via the org itself — is only live while the
// person is a CURRENT member of that organization.

// accessAll asserts that every entry point agrees on (user, project) → want.
func accessAll(t *testing.T, svc *Service, label string, userID, projectID uuid.UUID, want string) {
	t.Helper()
	ctx := context.Background()
	actor := Actor{UserID: userID}

	got, err := svc.effectiveProjectRole(ctx, actor, projectID)
	if err != nil {
		t.Fatalf("%s: effectiveProjectRole: %v", label, err)
	}
	if got != want {
		t.Errorf("%s: effectiveProjectRole = %q, want %q", label, got, want)
	}

	mine, err := svc.ListMyProjects(ctx, actor)
	if err != nil {
		t.Fatalf("%s: ListMyProjects: %v", label, err)
	}
	listedRole, listed := "", false
	for _, p := range mine {
		if p.ID == projectID {
			listed, listedRole = true, p.MemberRole
		}
	}
	if listed != (want != "") || listedRole != want {
		t.Errorf("%s: ListMyProjects listed=%v role=%q, want listed=%v role=%q", label, listed, listedRole, want != "", want)
	}

	visible, err := svc.visibleProjects(ctx, actor, "")
	if err != nil {
		t.Fatalf("%s: visibleProjects: %v", label, err)
	}
	seen := false
	for _, p := range visible {
		if p.ID == projectID {
			seen = true
		}
	}
	if seen != (want != "") {
		t.Errorf("%s: visibleProjects contains project = %v, want %v", label, seen, want != "")
	}
}

func TestProjectAccessMatrix(t *testing.T) {
	svc, st := scopeTestService(t)
	ctx := context.Background()
	founder := Actor{UserID: scopeMakeUser(t, st, "rfounder")}
	tenant, project, err := svc.CreateOrganization(ctx, founder, "resolver", "Resolver")
	if err != nil {
		t.Fatalf("create org: %v", err)
	}
	pool := st.Pool()

	member := scopeMakeUser(t, st, "rmember")
	if err := st.AddTenantMember(ctx, db.AddTenantMemberParams{TenantID: tenant.ID, UserID: member, Role: "member"}); err != nil {
		t.Fatal(err)
	}

	t.Run("org admin and member", func(t *testing.T) {
		accessAll(t, svc, "admin", founder.UserID, project.ID, "owner")
		accessAll(t, svc, "member", member, project.ID, "member")
	})

	t.Run("stranger", func(t *testing.T) {
		accessAll(t, svc, "stranger", scopeMakeUser(t, st, "rstranger"), project.ID, "")
	})

	t.Run("direct owner row without org membership is inert", func(t *testing.T) {
		// What an offboarded project creator looks like: the direct row outlived
		// the org membership.
		gone := scopeMakeUser(t, st, "rgone")
		if err := st.AddProjectMember(ctx, db.AddProjectMemberParams{ProjectID: project.ID, UserID: gone, Role: "owner"}); err != nil {
			t.Fatal(err)
		}
		accessAll(t, svc, "offboarded creator", gone, project.ID, "")
	})

	t.Run("group grant needs org membership", func(t *testing.T) {
		if _, err := svc.CreateGroup(ctx, founder, "resolver", "plat", "Platform", ""); err != nil {
			t.Fatal(err)
		}
		if err := svc.SetGroupProjectRole(ctx, founder, "resolver", "plat", "resolver", "owner"); err != nil {
			t.Fatal(err)
		}
		// An org member in the group gets the granted owner role.
		if err := svc.AddGroupMember(ctx, founder, "resolver", "plat", member); err != nil {
			t.Fatal(err)
		}
		accessAll(t, svc, "member via group", member, project.ID, "owner")

		// A non-member smuggled into the group (raw insert: the service refuses
		// it, see TestAddGroupMemberRequiresOrgMembership) gets nothing.
		outsider := scopeMakeUser(t, st, "routsider")
		if _, err := pool.Exec(ctx, `INSERT INTO group_members (group_id, user_id)
			SELECT id, $2 FROM groups WHERE tenant_id = $1 AND slug = 'plat'`, tenant.ID, outsider); err != nil {
			t.Fatal(err)
		}
		accessAll(t, svc, "non-member in group", outsider, project.ID, "")
	})

	t.Run("a group grant cannot reach another tenant's project", func(t *testing.T) {
		otherFounder := Actor{UserID: scopeMakeUser(t, st, "rother")}
		otherTenant, otherProject, err := svc.CreateOrganization(ctx, otherFounder, "resolver-b", "Resolver B")
		if err != nil {
			t.Fatal(err)
		}
		// dual belongs to both orgs as a plain member and sits in org A's group.
		dual := scopeMakeUser(t, st, "rdual")
		for _, id := range []uuid.UUID{tenant.ID, otherTenant.ID} {
			if err := st.AddTenantMember(ctx, db.AddTenantMemberParams{TenantID: id, UserID: dual, Role: "member"}); err != nil {
				t.Fatal(err)
			}
		}
		if err := svc.AddGroupMember(ctx, founder, "resolver", "plat", dual); err != nil {
			t.Fatal(err)
		}
		// Plant the row the service layer refuses to create (org A's group
		// granted owner on org B's project): the resolver must ignore it.
		if _, err := pool.Exec(ctx, `INSERT INTO group_project_roles (group_id, project_id, role)
			SELECT id, $2, 'owner' FROM groups WHERE tenant_id = $1 AND slug = 'plat'`, tenant.ID, otherProject.ID); err != nil {
			t.Fatal(err)
		}
		accessAll(t, svc, "dual in org B", dual, otherProject.ID, "member")
	})

	t.Run("personal project needs no org membership", func(t *testing.T) {
		owner := scopeMakeUser(t, st, "rpersonal")
		if err := svc.CreatePersonalProject(ctx, owner, "rpersonal"); err != nil {
			t.Fatal(err)
		}
		p, err := st.GetProjectBySlug(ctx, "rpersonal")
		if err != nil {
			t.Fatal(err)
		}
		accessAll(t, svc, "personal owner", owner, p.ID, "owner")
		accessAll(t, svc, "personal stranger", member, p.ID, "")
	})
}

func TestAddGroupMemberRequiresOrgMembership(t *testing.T) {
	svc, st := scopeTestService(t)
	ctx := context.Background()
	founder := Actor{UserID: scopeMakeUser(t, st, "gmfounder")}
	tenant, _, err := svc.CreateOrganization(ctx, founder, "gm-org", "GM Org")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateGroup(ctx, founder, "gm-org", "g", "G", ""); err != nil {
		t.Fatal(err)
	}

	outsider := scopeMakeUser(t, st, "gmoutsider")
	if err := svc.AddGroupMember(ctx, founder, "gm-org", "g", outsider); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("non-member: want ErrInvalidInput, got %v", err)
	}

	insider := scopeMakeUser(t, st, "gminsider")
	if err := st.AddTenantMember(ctx, db.AddTenantMemberParams{TenantID: tenant.ID, UserID: insider, Role: "member"}); err != nil {
		t.Fatal(err)
	}
	if err := svc.AddGroupMember(ctx, founder, "gm-org", "g", insider); err != nil {
		t.Fatalf("member: %v", err)
	}
}

// Offboarding — a manual removal by an org admin and SCIM deprovisioning share
// one primitive — must take the person's direct project rows and group
// memberships in THAT org with it, and leave everything else alone.
func TestOffboardingRemovesAccess(t *testing.T) {
	for _, via := range []string{"admin removal", "scim deprovision"} {
		t.Run(via, func(t *testing.T) {
			svc, st := scopeTestService(t)
			ctx := context.Background()
			founder := Actor{UserID: scopeMakeUser(t, st, "offfounder")}
			tenant, project, err := svc.CreateOrganization(ctx, founder, "off-org", "Off Org")
			if err != nil {
				t.Fatal(err)
			}
			alice := scopeMakeUser(t, st, "offalice")
			if err := st.AddTenantMember(ctx, db.AddTenantMemberParams{TenantID: tenant.ID, UserID: alice, Role: "member"}); err != nil {
				t.Fatal(err)
			}
			// Alice created a project in the org (direct owner row), is in a group,
			// and has her own personal project elsewhere.
			if err := st.AddProjectMember(ctx, db.AddProjectMemberParams{ProjectID: project.ID, UserID: alice, Role: "owner"}); err != nil {
				t.Fatal(err)
			}
			if _, err := svc.CreateGroup(ctx, founder, "off-org", "g", "G", ""); err != nil {
				t.Fatal(err)
			}
			if err := svc.AddGroupMember(ctx, founder, "off-org", "g", alice); err != nil {
				t.Fatal(err)
			}
			if err := svc.CreatePersonalProject(ctx, alice, "offalice"); err != nil {
				t.Fatal(err)
			}
			personal, err := st.GetProjectBySlug(ctx, "offalice")
			if err != nil {
				t.Fatal(err)
			}
			accessAll(t, svc, "before", alice, project.ID, "owner")

			if via == "admin removal" {
				err = svc.RemoveOrgMember(ctx, founder, "off-org", alice)
			} else {
				err = svc.scimDeprovision(ctx, tenant.ID, alice)
			}
			if err != nil {
				t.Fatalf("offboard: %v", err)
			}

			accessAll(t, svc, "after", alice, project.ID, "")
			accessAll(t, svc, "personal project untouched", alice, personal.ID, "owner")
			var rows int
			if err := st.Pool().QueryRow(ctx, `SELECT count(*) FROM project_members WHERE project_id = $1 AND user_id = $2`, project.ID, alice).Scan(&rows); err != nil || rows != 0 {
				t.Errorf("direct project rows left in the org: %d (err %v)", rows, err)
			}
			if err := st.Pool().QueryRow(ctx, `SELECT count(*) FROM group_members WHERE user_id = $1`, alice).Scan(&rows); err != nil || rows != 0 {
				t.Errorf("group memberships left: %d (err %v)", rows, err)
			}

			// Re-adding her must not resurrect the old owner role.
			if err := st.AddTenantMember(ctx, db.AddTenantMemberParams{TenantID: tenant.ID, UserID: alice, Role: "member"}); err != nil {
				t.Fatal(err)
			}
			accessAll(t, svc, "rejoined", alice, project.ID, "member")
		})
	}
}
