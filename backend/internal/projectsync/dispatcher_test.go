package projectsync

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nielsuitterdijk22/quill/internal/store/db"
)

// clock is a manually-advanced time source used by zitadel_token_test.go to
// drive proactive token refresh deterministically.
type clock struct {
	mu sync.Mutex
	t  time.Time
}

func newClock() *clock { return &clock{t: time.Unix(1_700_000_000, 0).UTC()} }

func (c *clock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// The dispatcher's poll/backoff/retry/disabled behavior is exercised once, over
// the shared engine, in internal/outbox. Here we only assert the project-sync
// adapter delivers a project event carrying the fields Quill owns and nothing it
// doesn't (no key/visibility smuggled).

// fakeOutbox is a minimal in-memory project_sync_outbox.
type fakeOutbox struct {
	mu   sync.Mutex
	rows []*db.ProjectSyncOutbox
}

func (f *fakeOutbox) enqueue(ev Event) {
	payload, _ := json.Marshal(ev)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rows = append(f.rows, &db.ProjectSyncOutbox{ID: ev.EventID, Payload: payload})
}

func (f *fakeOutbox) ListPendingProjectSyncEvents(_ context.Context, limit int32) ([]db.ProjectSyncOutbox, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []db.ProjectSyncOutbox
	for _, r := range f.rows {
		if r.DeliveredAt.Valid {
			continue
		}
		out = append(out, *r)
		if int32(len(out)) >= limit {
			break
		}
	}
	return out, nil
}

func (f *fakeOutbox) MarkProjectSyncEventDelivered(_ context.Context, id uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, r := range f.rows {
		if r.ID == id {
			r.DeliveredAt = pgtype.Timestamptz{Valid: true}
		}
	}
	return nil
}

func (f *fakeOutbox) MarkProjectSyncEventFailed(_ context.Context, _ db.MarkProjectSyncEventFailedParams) error {
	return nil
}

func TestProjectEventDeliversOwnedFieldsOnly(t *testing.T) {
	var gotBody Event
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	out := &fakeOutbox{}
	ev := NewEvent(EventCreate, uuid.New(), "payments", "Payments", "default")
	out.enqueue(ev)
	d := NewDispatcher(Config{URL: srv.URL}, out, StaticTokenSource(""), nil)

	if n, err := d.ProcessBatch(context.Background()); err != nil || n != 1 {
		t.Fatalf("ProcessBatch: n=%d err=%v (want 1, nil)", n, err)
	}
	if gotBody.EventID != ev.EventID || gotBody.Slug != "payments" || gotBody.TenantSlug != "default" {
		t.Fatalf("delivered body mismatch: %+v", gotBody)
	}
	// Quill owns no key/visibility, so the payload must not smuggle them.
	if gotBody.EventType != EventCreate || gotBody.Deleted || gotBody.Archived {
		t.Fatalf("unexpected lifecycle flags: %+v", gotBody)
	}
}
