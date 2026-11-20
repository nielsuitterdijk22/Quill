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
	"github.com/jackc/pgx/v5"

	"github.com/nielsuitterdijk22/quill/internal/store/db"
)

func (f *fakeOutbox) GetProjectByID(_ context.Context, id uuid.UUID) (db.Project, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.snapshots[id]; !ok {
		return db.Project{}, pgx.ErrNoRows
	}
	return db.Project{ID: id}, nil
}

func (f *fakeOutbox) ListProjectAccessSnapshot(_ context.Context, id uuid.UUID) ([]db.ListProjectAccessSnapshotRow, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.snapshots[id], nil
}

func (f *fakeOutbox) MarkProjectSyncEventSnapshot(_ context.Context, id uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.marked = append(f.marked, id)
	return nil
}

func (f *fakeOutbox) enqueueAccess(projectID uuid.UUID) uuid.UUID {
	f.mu.Lock()
	defer f.mu.Unlock()
	id := uuid.New()
	f.rows = append(f.rows, &db.ProjectSyncOutbox{ID: id, ProjectID: projectID, EventType: string(EventAccess), Payload: []byte("{}"), OccurredAt: time.Now().UTC()})
	return id
}

// An 'access' row is stored empty and filled at delivery: the payload Tempo gets
// is the current roster plus resolved grants (people with no access are members,
// not grantees); the row is marked as read first; a project that no longer exists
// is retired without a request.
func TestAccessEventPayloadIsBuiltAtDelivery(t *testing.T) {
	alice, bob := uuid.New(), uuid.New()
	project, gone := uuid.New(), uuid.New()
	out := &fakeOutbox{snapshots: map[uuid.UUID][]db.ListProjectAccessSnapshotRow{
		project: {
			{UserID: alice, Username: "alice", Email: "a@x.eu", DisplayName: "Alice", TenantRole: "owner", Role: "owner"},
			{UserID: bob, Username: "bob", Email: "b@x.eu", DisplayName: "Bob", TenantRole: "member", Role: ""},
		},
	}}
	live := out.enqueueAccess(project)
	out.enqueueAccess(gone)

	var mu sync.Mutex
	var bodies []AccessEvent
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var ev AccessEvent
		_ = json.NewDecoder(r.Body).Decode(&ev)
		mu.Lock()
		bodies = append(bodies, ev)
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	d := NewDispatcher(Config{URL: srv.URL}, out, StaticTokenSource(""), nil)
	if _, err := d.ProcessBatch(context.Background()); err != nil {
		t.Fatal(err)
	}

	if len(bodies) != 1 {
		t.Fatalf("one request expected (the vanished project sends nothing), got %d", len(bodies))
	}
	got := bodies[0]
	if got.EventType != EventAccess || got.ProjectID != project || got.EventID != live {
		t.Errorf("envelope: %+v", got)
	}
	if len(got.Members) != 2 || got.Members[0].Username != "alice" || got.Members[0].TenantRole != "owner" {
		t.Errorf("members: %+v", got.Members)
	}
	if len(got.Grants) != 1 || got.Grants[0].UserID != alice || got.Grants[0].Role != "owner" {
		t.Errorf("only people with a role are granted: %+v", got.Grants)
	}
	if len(out.marked) != 2 {
		t.Errorf("every access event is marked as read before its snapshot: %v", out.marked)
	}
	for _, r := range out.rows {
		if !r.DeliveredAt.Valid {
			t.Errorf("event %s must be delivered or retired", r.ID)
		}
	}
}
