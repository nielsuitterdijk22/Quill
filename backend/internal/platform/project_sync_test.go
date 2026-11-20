package platform_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/nielsuitterdijk22/quill/internal/platform"
	"github.com/nielsuitterdijk22/quill/internal/store/db"
)

// TestCreateProjectEnqueuesSyncEvent verifies the real create path writes a
// project-mirror outbox row alongside the project (same transaction), carrying
// the identity Tempo mirrors: id, slug, name, tenant slug.
func TestCreateProjectEnqueuesSyncEvent(t *testing.T) {
	svc, st := newService(t)
	ctx := context.Background()
	creator := makeUser(t, st, "syncer")

	project, err := svc.CreateProject(ctx, actor(creator), platform.CreateProjectInput{Slug: "mirrored", Name: "Mirrored"})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}

	rows, err := st.ListPendingProjectSyncEvents(ctx, 10)
	if err != nil {
		t.Fatalf("list pending sync events: %v", err)
	}
	// Creating a project queues its create event, plus one access event: the
	// creator becomes the first owner, and Tempo mirrors who can reach it.
	var creates, accesses []db.ProjectSyncOutbox
	for _, r := range rows {
		switch r.EventType {
		case "create":
			creates = append(creates, r)
		case "access":
			accesses = append(accesses, r)
		default:
			t.Fatalf("unexpected event type %q", r.EventType)
		}
	}
	if len(creates) != 1 || len(accesses) != 1 {
		t.Fatalf("expected 1 create and 1 access event, got %d create, %d access", len(creates), len(accesses))
	}
	row := creates[0]
	if row.ProjectID != project.ID || accesses[0].ProjectID != project.ID {
		t.Fatalf("unexpected event rows: %+v / %+v", row, accesses[0])
	}
	var payload struct {
		Slug       string `json:"slug"`
		Name       string `json:"name"`
		TenantSlug string `json:"tenantSlug"`
		Deleted    bool   `json:"deleted"`
	}
	if err := json.Unmarshal(row.Payload, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if payload.Slug != "mirrored" || payload.Name != "Mirrored" || payload.TenantSlug != "default" || payload.Deleted {
		t.Fatalf("payload mismatch: %+v", payload)
	}
}

// TestPurgeOwnedProjectsEnqueuesDeleteEvent verifies account-deletion purge
// emits a delete event for the removed project, in the same transaction as the
// project row deletion, so Tempo can archive its mirror.
func TestPurgeOwnedProjectsEnqueuesDeleteEvent(t *testing.T) {
	svc, st := newService(t)
	ctx := context.Background()
	uid := makeUser(t, st, "leaver")

	project, err := svc.CreateProject(ctx, actor(uid), platform.CreateProjectInput{Slug: "doomed", Name: "Doomed"})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	if err := svc.PurgeOwnedProjects(ctx, uid); err != nil {
		t.Fatalf("purge: %v", err)
	}
	if _, err := st.GetProjectByID(ctx, project.ID); err == nil {
		t.Fatal("project should be deleted")
	}

	// The create event (from CreateProject) and the delete event (from the purge)
	// both exist; the delete one must reference the purged project.
	rows, err := st.ListPendingProjectSyncEvents(ctx, 10)
	if err != nil {
		t.Fatalf("list pending sync events: %v", err)
	}
	var sawDelete bool
	for _, row := range rows {
		if row.ProjectID == project.ID && row.EventType == "delete" {
			sawDelete = true
		}
	}
	if !sawDelete {
		t.Fatalf("expected a delete event for project %s, got %+v", project.ID, rows)
	}
}
