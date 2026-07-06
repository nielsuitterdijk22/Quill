package platform_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/nielsuitterdijk22/quill/internal/platform"
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
	if len(rows) != 1 {
		t.Fatalf("expected 1 pending sync event, got %d", len(rows))
	}
	row := rows[0]
	if row.ProjectID != project.ID || row.EventType != "create" {
		t.Fatalf("unexpected event row: %+v", row)
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
