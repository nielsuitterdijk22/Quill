package platform_test

import (
	"context"
	"testing"

	"github.com/nielsuitterdijk22/quill/internal/platform"
	"github.com/nielsuitterdijk22/quill/internal/store/db"
)

// TestCreatePersonalProjectGrantsMembership verifies a personal project is
// created with the caller as owner, visible via the membership-scoped list the
// dashboard/onboarding relies on.
func TestCreatePersonalProjectGrantsMembership(t *testing.T) {
	svc, st := newService(t)
	ctx := context.Background()
	uid := makeUser(t, st, "solo")

	if err := svc.CreatePersonalProject(ctx, uid, "solo"); err != nil {
		t.Fatalf("create personal project: %v", err)
	}

	mine, err := svc.ListMyProjects(ctx, actor(uid))
	if err != nil {
		t.Fatalf("list my projects: %v", err)
	}
	if len(mine) != 1 || mine[0].Slug != "solo" || !mine[0].IsPersonal {
		t.Fatalf("expected one personal project 'solo', got %+v", mine)
	}
}

// TestPurgeOwnedProjectsRemovesReposAndProjects verifies account-deletion
// erasure: a solo-owned project and its repositories are fully removed so a
// later re-signup can re-import the same repos without conflicts.
func TestPurgeOwnedProjectsRemovesReposAndProjects(t *testing.T) {
	svc, st := newService(t)
	ctx := context.Background()
	uid := makeUser(t, st, "owner")

	if err := svc.CreatePersonalProject(ctx, uid, "owner"); err != nil {
		t.Fatalf("create personal project: %v", err)
	}
	if _, err := svc.CreateRepo(ctx, actor(uid), "owner", platform.CreateRepoInput{Slug: "widget", Name: "Widget"}); err != nil {
		t.Fatalf("create repo: %v", err)
	}

	if err := svc.PurgeOwnedProjects(ctx, uid); err != nil {
		t.Fatalf("purge: %v", err)
	}

	// Project (and thus its repos) must be gone.
	if mine, err := svc.ListMyProjects(ctx, actor(uid)); err != nil || len(mine) != 0 {
		t.Fatalf("expected no projects after purge, got %+v (err=%v)", mine, err)
	}
	// The slug is free again — a fresh personal project can be created.
	if err := svc.CreatePersonalProject(ctx, uid, "owner"); err != nil {
		t.Fatalf("re-create personal project after purge: %v", err)
	}
	if _, err := svc.CreateRepo(ctx, actor(uid), "owner", platform.CreateRepoInput{Slug: "widget", Name: "Widget"}); err != nil {
		t.Fatalf("re-create repo after purge should succeed (no conflict), got: %v", err)
	}
}

// TestPurgeOwnedProjectsKeepsSharedProjects verifies a non-personal project with
// other members is left intact when one member's account is purged.
func TestPurgeOwnedProjectsKeepsSharedProjects(t *testing.T) {
	svc, st := newService(t)
	ctx := context.Background()
	owner := makeUser(t, st, "alice")
	other := makeUser(t, st, "bob")

	proj, err := svc.CreateProject(ctx, platform.Actor{UserID: owner}, platform.CreateProjectInput{Slug: "acme", Name: "Acme"})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	if err := st.AddProjectMember(ctx, db.AddProjectMemberParams{ProjectID: proj.ID, UserID: other, Role: "member"}); err != nil {
		t.Fatalf("add second member: %v", err)
	}

	if err := svc.PurgeOwnedProjects(ctx, owner); err != nil {
		t.Fatalf("purge: %v", err)
	}

	// Shared project survives; the remaining member can still see it.
	if mine, err := svc.ListMyProjects(ctx, actor(other)); err != nil || len(mine) != 1 || mine[0].Slug != "acme" {
		t.Fatalf("shared project should remain for other member, got %+v (err=%v)", mine, err)
	}
}

// TestCreatePersonalProjectRepairsMembership verifies that when a project with
// the personal slug exists WITHOUT the membership row (e.g. a partially
// completed earlier run), re-running provisioning repairs access instead of
// silently succeeding. A missing membership previously surfaced as a 404 on
// GitHub import.
func TestCreatePersonalProjectRepairsMembership(t *testing.T) {
	svc, st := newService(t)
	ctx := context.Background()
	uid := makeUser(t, st, "repair")

	if err := svc.CreatePersonalProject(ctx, uid, "repair"); err != nil {
		t.Fatalf("first create: %v", err)
	}
	mine, err := svc.ListMyProjects(ctx, actor(uid))
	if err != nil || len(mine) != 1 {
		t.Fatalf("setup: expected one project, got %+v (err=%v)", mine, err)
	}

	// Simulate a dangling project: drop the membership row but keep the project.
	if err := st.RemoveProjectMember(ctx, db.RemoveProjectMemberParams{
		ProjectID: mine[0].ID,
		UserID:    uid,
	}); err != nil {
		t.Fatalf("remove member: %v", err)
	}
	if again, _ := svc.ListMyProjects(ctx, actor(uid)); len(again) != 0 {
		t.Fatalf("setup: membership should be gone, got %+v", again)
	}

	// Re-running provisioning must restore access rather than no-op.
	if err := svc.CreatePersonalProject(ctx, uid, "repair"); err != nil {
		t.Fatalf("repair create: %v", err)
	}
	repaired, err := svc.ListMyProjects(ctx, actor(uid))
	if err != nil {
		t.Fatalf("list after repair: %v", err)
	}
	if len(repaired) != 1 || repaired[0].Slug != "repair" {
		t.Fatalf("membership should be repaired, got %+v", repaired)
	}
}
