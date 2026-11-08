package platform

import (
	"context"
	"testing"

	"github.com/nielsuitterdijk22/quill/internal/store/db"
)

// The cross-cutting views (Pulls overview, dashboard PR count) list only repos
// in projects the actor can actually reach through membership — direct
// project_members or an org tenant_members row. Notably a platform admin with
// no memberships sees nothing here: admin visibility is for admin tooling, not
// for the user's personal dashboard or PR views. This mirrors the "PRs in my
// account while I don't have repos" incident (2026-11-08): the platform admin
// saw every project's PRs in their personal /pulls page.
func visibleProjectSlugs(t *testing.T, svc *Service, actor Actor) map[string]bool {
	t.Helper()
	projects, err := svc.visibleProjects(context.Background(), actor, "")
	if err != nil {
		t.Fatalf("visibleProjects: %v", err)
	}
	out := map[string]bool{}
	for _, p := range projects {
		out[p.Slug] = true
	}
	return out
}

func TestVisibleProjectsScopesToMemberships(t *testing.T) {
	svc, st := scopeTestService(t)
	ctx := context.Background()

	// An org founder, an invited org member, a personal-only user, and a
	// platform admin with no project memberships at all.
	founder := Actor{UserID: scopeMakeUser(t, st, "vfounder")}
	if _, _, err := svc.CreateOrganization(ctx, founder, "vorg", "V Org"); err != nil {
		t.Fatalf("create org: %v", err)
	}
	member := Actor{UserID: scopeMakeUser(t, st, "vmember")}
	tenant, err := st.GetTenantBySlug(ctx, "vorg")
	if err != nil {
		t.Fatalf("lookup tenant: %v", err)
	}
	if err := st.AddTenantMember(ctx, db.AddTenantMemberParams{TenantID: tenant.ID, UserID: member.UserID, Role: "member"}); err != nil {
		t.Fatalf("add tenant member: %v", err)
	}
	outsiderID := scopeMakeUser(t, st, "voutsider")
	if err := svc.CreatePersonalProject(ctx, outsiderID, "voutsider"); err != nil {
		t.Fatalf("personal project: %v", err)
	}
	adminNoMembers := Actor{UserID: scopeMakeUser(t, st, "vadmin"), IsAdmin: true}

	// The org founder (tenant admin) and the invited member both see the org
	// project only.
	for name, actor := range map[string]Actor{"founder": founder, "org member": member} {
		got := visibleProjectSlugs(t, svc, actor)
		if len(got) != 1 || !got["vorg"] {
			t.Fatalf("%s: visible projects = %v, want [vorg]", name, got)
		}
	}

	// The personal user sees only their personal project.
	got := visibleProjectSlugs(t, svc, Actor{UserID: outsiderID})
	if len(got) != 1 || !got["voutsider"] {
		t.Fatalf("personal user: visible projects = %v, want [voutsider]", got)
	}

	// The platform admin has no memberships: nothing is visible in the
	// cross-cutting user views. (Previously authorizeProjectMember returned
	// 'owner' for admins and exposed every project on the platform.)
	if got := visibleProjectSlugs(t, svc, adminNoMembers); len(got) != 0 {
		t.Fatalf("memberless admin: visible projects = %v, want none", got)
	}

	// A user with both an org and a personal project sees both.
	if err := svc.CreatePersonalProject(ctx, founder.UserID, "vfounder"); err != nil {
		t.Fatalf("founder personal project: %v", err)
	}
	got = visibleProjectSlugs(t, svc, founder)
	if len(got) != 2 || !got["vorg"] || !got["vfounder"] {
		t.Fatalf("founder with personal project: visible projects = %v, want [vorg vfounder]", got)
	}
}

// The pulls overview must respect an explicit ?project= filter, but only for
// projects the actor can actually see; a filter naming a foreign project yields
// nothing rather than leaking it.
func TestVisibleProjectsProjectFilter(t *testing.T) {
	svc, st := scopeTestService(t)
	ctx := context.Background()

	founder := Actor{UserID: scopeMakeUser(t, st, "ffounder")}
	if _, _, err := svc.CreateOrganization(ctx, founder, "forg", "F Org"); err != nil {
		t.Fatalf("create org: %v", err)
	}
	outsider := Actor{UserID: scopeMakeUser(t, st, "foutsider")}
	if err := svc.CreatePersonalProject(ctx, outsider.UserID, "foutsider"); err != nil {
		t.Fatalf("personal project: %v", err)
	}

	// Founder filtering to their org gets it; outsider filtering to the same
	// foreign org gets nothing.
	projects, err := svc.visibleProjects(ctx, founder, "forg")
	if err != nil || len(projects) != 1 || projects[0].Slug != "forg" {
		t.Fatalf("founder filter: projects=%v err=%v, want [forg]", projects, err)
	}
	projects, err = svc.visibleProjects(ctx, outsider, "forg")
	if err != nil || len(projects) != 0 {
		t.Fatalf("outsider foreign filter: projects=%v err=%v, want none", projects, err)
	}
}

// /me/projects (the sidebar switcher source) must include projects the user
// reaches through org membership (tenant_members), not just direct
// project_members rows — otherwise an invited org member can open the org's
// repos by URL but never switch to the org project in the UI.
func TestListMyProjectsIncludesOrgProjects(t *testing.T) {
	svc, st := scopeTestService(t)
	ctx := context.Background()

	founder := Actor{UserID: scopeMakeUser(t, st, "mfounder")}
	if _, _, err := svc.CreateOrganization(ctx, founder, "morg", "M Org"); err != nil {
		t.Fatalf("create org: %v", err)
	}
	if err := svc.CreatePersonalProject(ctx, founder.UserID, "mfounder"); err != nil {
		t.Fatalf("personal project: %v", err)
	}
	member := Actor{UserID: scopeMakeUser(t, st, "mmember")}
	tenant, err := st.GetTenantBySlug(ctx, "morg")
	if err != nil {
		t.Fatalf("lookup tenant: %v", err)
	}
	if err := st.AddTenantMember(ctx, db.AddTenantMemberParams{TenantID: tenant.ID, UserID: member.UserID, Role: "member"}); err != nil {
		t.Fatalf("add tenant member: %v", err)
	}

	// Founder: own role on both their projects.
	rows, err := svc.ListMyProjects(ctx, founder)
	if err != nil {
		t.Fatalf("ListMyProjects: %v", err)
	}
	slugs := map[string]string{}
	for _, r := range rows {
		slugs[r.Slug] = r.MemberRole
	}
	if slugs["mfounder"] != "owner" || slugs["morg"] != "owner" {
		t.Fatalf("founder roles = %v, want owner on mfounder and morg", slugs)
	}

	// Invited member: the org project appears with the member role despite
	// having no direct project_members row.
	rows, err = svc.ListMyProjects(ctx, member)
	if err != nil {
		t.Fatalf("ListMyProjects (member): %v", err)
	}
	if len(rows) != 1 || rows[0].Slug != "morg" || rows[0].MemberRole != "member" {
		t.Fatalf("member projects = %+v, want [{morg member}]", rows)
	}

	// A platform admin with no memberships gets nothing (no broadening).
	rows, err = svc.ListMyProjects(ctx, Actor{UserID: scopeMakeUser(t, st, "madmin"), IsAdmin: true})
	if err != nil {
		t.Fatalf("ListMyProjects (admin): %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("memberless admin projects = %+v, want none", rows)
	}
}
