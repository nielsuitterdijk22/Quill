package platform_test

import (
	"context"
	"testing"

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
