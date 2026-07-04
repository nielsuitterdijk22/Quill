package projectsync_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"

	"github.com/google/uuid"

	"github.com/nielsuitterdijk22/quill/internal/projectsync"
	"github.com/nielsuitterdijk22/quill/internal/store"
	"github.com/nielsuitterdijk22/quill/internal/store/db"
)

// These tests exercise the outbox against a live Postgres, following the same
// convention as the store/platform integration tests: skipped unless
// QUILL_TEST_DATABASE_URL is set so `go test ./...` stays green without a DB.
func testStore(t *testing.T) *store.Store {
	t.Helper()
	dsn := os.Getenv("QUILL_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("QUILL_TEST_DATABASE_URL not set; skipping projectsync integration test")
	}
	if err := store.Migrate(dsn); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	st, err := store.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(st.Close)
	reset := func() {
		_, _ = st.Pool().Exec(context.Background(), "TRUNCATE projects, project_sync_outbox CASCADE")
	}
	reset()
	t.Cleanup(reset)
	return st
}

func defaultTenant(t *testing.T, st *store.Store) db.Tenant {
	t.Helper()
	tenant, err := st.GetTenantBySlug(context.Background(), "default")
	if err != nil {
		t.Fatalf("get default tenant: %v", err)
	}
	return tenant
}

// TestOutboxRowCommitsWithMutation verifies the core outbox property in both
// directions: when the transaction holding the project mutation commits the
// event row exists, and when it rolls back no event is left behind.
func TestOutboxRowCommitsWithMutation(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	tenant := defaultTenant(t, st)

	// Committed path: project + event land together.
	var projectID uuid.UUID
	err := st.InTx(ctx, func(q *db.Queries) error {
		p, err := q.CreateProject(ctx, db.CreateProjectParams{
			TenantID: tenant.ID, Slug: "sync-commit", Name: "Sync Commit",
		})
		if err != nil {
			return err
		}
		projectID = p.ID
		return projectsync.Enqueue(ctx, q, projectsync.NewEvent(
			projectsync.EventCreate, p.ID, p.Slug, p.Name, tenant.Slug))
	})
	if err != nil {
		t.Fatalf("committed tx: %v", err)
	}
	if n, err := st.CountProjectSyncEventsByProject(ctx, projectID); err != nil || n != 1 {
		t.Fatalf("expected 1 event after commit, got n=%d err=%v", n, err)
	}

	// Rolled-back path: the mutation fails after the enqueue -> no event.
	sentinel := errors.New("boom")
	var rolledBackID uuid.UUID
	err = st.InTx(ctx, func(q *db.Queries) error {
		p, err := q.CreateProject(ctx, db.CreateProjectParams{
			TenantID: tenant.ID, Slug: "sync-rollback", Name: "Sync Rollback",
		})
		if err != nil {
			return err
		}
		rolledBackID = p.ID
		if err := projectsync.Enqueue(ctx, q, projectsync.NewEvent(
			projectsync.EventCreate, p.ID, p.Slug, p.Name, tenant.Slug)); err != nil {
			return err
		}
		return sentinel // force rollback after both writes
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected sentinel error, got %v", err)
	}
	if _, err := st.GetProjectBySlug(ctx, "sync-rollback"); err == nil {
		t.Fatal("rolled-back project must not exist")
	}
	if n, err := st.CountProjectSyncEventsByProject(ctx, rolledBackID); err != nil || n != 0 {
		t.Fatalf("rollback must leave no outbox event, got n=%d err=%v", n, err)
	}
}

// TestOutboxPayloadShape verifies the persisted payload carries exactly the
// mirrored identity Tempo dedupes and derives from: event id, occurred_at,
// project id, slug, name, tenant slug, lifecycle flags — and no key/visibility
// (Quill doesn't own those; Tempo derives them on intake).
func TestOutboxPayloadShape(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	tenant := defaultTenant(t, st)

	ev := projectsync.NewEvent(projectsync.EventDelete, uuid.New(), "payments", "Payments", tenant.Slug)
	if err := projectsync.Enqueue(ctx, st, ev); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	rows, err := st.ListPendingProjectSyncEvents(ctx, 10)
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 pending row, got %d", len(rows))
	}
	row := rows[0]
	if row.ID != ev.EventID || row.ProjectID != ev.ProjectID || row.EventType != "delete" {
		t.Fatalf("row identity mismatch: %+v", row)
	}

	var payload map[string]any
	if err := json.Unmarshal(row.Payload, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	for _, key := range []string{"eventId", "eventType", "occurredAt", "projectId", "slug", "name", "tenantSlug", "archived", "deleted"} {
		if _, ok := payload[key]; !ok {
			t.Errorf("payload missing %q: %v", key, payload)
		}
	}
	for _, forbidden := range []string{"key", "visibility"} {
		if _, ok := payload[forbidden]; ok {
			t.Errorf("payload must not carry %q (Tempo derives it on intake)", forbidden)
		}
	}
	if payload["deleted"] != true || payload["archived"] != false {
		t.Errorf("delete event flags wrong: %v", payload)
	}
}

// TestBackfillIsIdempotent runs the backfill twice over pre-existing projects
// and asserts the second run enqueues nothing, so replay can't double-deliver.
func TestBackfillIsIdempotent(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	tenant := defaultTenant(t, st)

	// Two pre-existing projects with no outbox history (pre-date the mirror).
	for _, slug := range []string{"legacy-a", "legacy-b"} {
		if _, err := st.CreateProject(ctx, db.CreateProjectParams{
			TenantID: tenant.ID, Slug: slug, Name: slug,
		}); err != nil {
			t.Fatalf("create project %s: %v", slug, err)
		}
	}

	first, err := projectsync.Backfill(ctx, st, nil)
	if err != nil {
		t.Fatalf("first backfill: %v", err)
	}
	if first.Total != 2 || first.Enqueued != 2 || first.Skipped != 0 {
		t.Fatalf("first backfill: %+v", first)
	}

	second, err := projectsync.Backfill(ctx, st, nil)
	if err != nil {
		t.Fatalf("second backfill: %v", err)
	}
	if second.Total != 2 || second.Enqueued != 0 || second.Skipped != 2 {
		t.Fatalf("second backfill must skip everything: %+v", second)
	}

	// Exactly one pending event per project — nothing double-enqueued.
	rows, err := st.ListPendingProjectSyncEvents(ctx, 100)
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 pending events, got %d", len(rows))
	}
	seen := map[uuid.UUID]int{}
	for _, r := range rows {
		seen[r.ProjectID]++
	}
	for pid, n := range seen {
		if n != 1 {
			t.Fatalf("project %s has %d events, want 1", pid, n)
		}
	}
}
