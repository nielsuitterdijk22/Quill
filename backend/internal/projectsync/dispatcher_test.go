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

// clock is a manually-advanced time source shared by the dispatcher and the
// fake outbox so backoff scheduling can be exercised deterministically.
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

// fakeOutbox is an in-memory Outbox so dispatcher tests need no database.
type fakeOutbox struct {
	mu    sync.Mutex
	rows  []*db.ProjectSyncOutbox
	clock *clock
}

func (f *fakeOutbox) enqueue(ev Event) {
	payload, _ := json.Marshal(ev)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rows = append(f.rows, &db.ProjectSyncOutbox{
		ID:            ev.EventID,
		ProjectID:     ev.ProjectID,
		EventType:     string(ev.EventType),
		Payload:       payload,
		OccurredAt:    ev.OccurredAt,
		NextAttemptAt: f.clock.now(),
	})
}

func (f *fakeOutbox) ListPendingProjectSyncEvents(_ context.Context, limit int32) ([]db.ProjectSyncOutbox, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	now := f.clock.now()
	var out []db.ProjectSyncOutbox
	for _, r := range f.rows {
		if r.DeliveredAt.Valid || r.NextAttemptAt.After(now) {
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
			r.DeliveredAt = pgtype.Timestamptz{Time: f.clock.now(), Valid: true}
		}
	}
	return nil
}

func (f *fakeOutbox) MarkProjectSyncEventFailed(_ context.Context, arg db.MarkProjectSyncEventFailedParams) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, r := range f.rows {
		if r.ID == arg.ID {
			r.Attempts++
			r.NextAttemptAt = arg.NextAttemptAt
		}
	}
	return nil
}

func (f *fakeOutbox) get(id uuid.UUID) *db.ProjectSyncOutbox {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, r := range f.rows {
		if r.ID == id {
			cp := *r
			return &cp
		}
	}
	return nil
}

// testDispatcher wires a dispatcher against the fake outbox and shared clock.
func testDispatcher(t *testing.T, url, token string, out *fakeOutbox) *Dispatcher {
	t.Helper()
	d := NewDispatcher(Config{
		URL:         url,
		BaseBackoff: time.Minute,
		MaxBackoff:  time.Hour,
	}, out, StaticTokenSource(token), nil)
	d.now = out.clock.now
	return d
}

func sampleEvent() Event {
	return NewEvent(EventCreate, uuid.New(), "payments", "Payments", "default")
}

func TestDispatcherDeliversOnSuccess(t *testing.T) {
	var (
		mu       sync.Mutex
		requests int
		gotAuth  string
		gotBody  Event
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests++
		gotAuth = r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	out := &fakeOutbox{clock: newClock()}
	ev := sampleEvent()
	out.enqueue(ev)
	d := testDispatcher(t, srv.URL, "secret-token", out)

	n, err := d.ProcessBatch(context.Background())
	if err != nil {
		t.Fatalf("ProcessBatch: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 delivered, got %d", n)
	}
	if row := out.get(ev.EventID); row == nil || !row.DeliveredAt.Valid {
		t.Fatalf("event should be marked delivered, got %+v", row)
	}
	mu.Lock()
	defer mu.Unlock()
	if requests != 1 {
		t.Fatalf("expected 1 request, got %d", requests)
	}
	if gotAuth != "Bearer secret-token" {
		t.Fatalf("expected bearer auth header, got %q", gotAuth)
	}
	if gotBody.EventID != ev.EventID || gotBody.Slug != "payments" || gotBody.TenantSlug != "default" {
		t.Fatalf("delivered body mismatch: %+v", gotBody)
	}
	// Quill owns no key/visibility, so the payload must not smuggle them.
	if gotBody.EventType != EventCreate || gotBody.Deleted || gotBody.Archived {
		t.Fatalf("unexpected lifecycle flags: %+v", gotBody)
	}
}

func TestDispatcher500ThenSuccess(t *testing.T) {
	var (
		mu   sync.Mutex
		fail = true
		hits int
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		hits++
		shouldFail := fail
		mu.Unlock()
		if shouldFail {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	out := &fakeOutbox{clock: newClock()}
	ev := sampleEvent()
	out.enqueue(ev)
	d := testDispatcher(t, srv.URL, "", out)

	// First pass: Tempo returns 500 -> failed, retry scheduled in the future.
	if n, err := d.ProcessBatch(context.Background()); err != nil || n != 0 {
		t.Fatalf("first pass: n=%d err=%v (want 0, nil)", n, err)
	}
	row := out.get(ev.EventID)
	if row.DeliveredAt.Valid {
		t.Fatal("event must not be delivered after a 500")
	}
	if row.Attempts != 1 {
		t.Fatalf("expected attempts=1, got %d", row.Attempts)
	}
	// Not yet due: another pass at the same instant delivers nothing.
	if n, _ := d.ProcessBatch(context.Background()); n != 0 {
		t.Fatalf("event should not be due yet, delivered %d", n)
	}

	// Time passes and Tempo recovers.
	out.clock.advance(2 * time.Minute)
	mu.Lock()
	fail = false
	mu.Unlock()

	if n, err := d.ProcessBatch(context.Background()); err != nil || n != 1 {
		t.Fatalf("retry pass: n=%d err=%v (want 1, nil)", n, err)
	}
	if row := out.get(ev.EventID); !row.DeliveredAt.Valid {
		t.Fatal("event should be delivered after Tempo recovered")
	}
	mu.Lock()
	defer mu.Unlock()
	if hits != 2 {
		t.Fatalf("expected 2 delivery attempts, got %d", hits)
	}
}

func TestDispatcherSurvivesTempoDown(t *testing.T) {
	// A server that is closed before use simulates Tempo being unreachable.
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close()

	out := &fakeOutbox{clock: newClock()}
	ev := sampleEvent()
	out.enqueue(ev)
	d := testDispatcher(t, url, "", out)

	n, err := d.ProcessBatch(context.Background())
	if err != nil {
		t.Fatalf("ProcessBatch should not error on transport failure: %v", err)
	}
	if n != 0 {
		t.Fatalf("expected 0 delivered while Tempo is down, got %d", n)
	}
	row := out.get(ev.EventID)
	if row == nil {
		t.Fatal("event row must survive Tempo downtime")
	}
	if row.DeliveredAt.Valid {
		t.Fatal("event must not be marked delivered while Tempo is down")
	}
	if row.Attempts != 1 {
		t.Fatalf("expected attempts=1 after a failed attempt, got %d", row.Attempts)
	}
	if !row.NextAttemptAt.After(out.clock.now()) {
		t.Fatal("a retry should be scheduled in the future")
	}
}

func TestDispatcherDisabledWhenURLEmpty(t *testing.T) {
	out := &fakeOutbox{clock: newClock()}
	out.enqueue(sampleEvent())
	d := NewDispatcher(Config{URL: ""}, out, nil, nil)

	if d.Enabled() {
		t.Fatal("dispatcher must be disabled when URL is empty")
	}
	// Run must return promptly (idle) rather than block or poll.
	done := make(chan struct{})
	go func() { d.Run(context.Background()); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run should return immediately when sync is disabled")
	}
	// Nothing was delivered.
	for _, r := range out.rows {
		if r.DeliveredAt.Valid {
			t.Fatal("disabled dispatcher must not deliver events")
		}
	}
}

func TestBackoffDoublesAndCaps(t *testing.T) {
	d := NewDispatcher(Config{URL: "x", BaseBackoff: time.Second, MaxBackoff: 10 * time.Second}, nil, nil, nil)
	cases := []struct {
		attempts int32
		want     time.Duration
	}{
		{0, time.Second},
		{1, 2 * time.Second},
		{2, 4 * time.Second},
		{3, 8 * time.Second},
		{4, 10 * time.Second}, // capped
		{10, 10 * time.Second},
	}
	for _, c := range cases {
		if got := d.backoff(c.attempts); got != c.want {
			t.Errorf("backoff(%d) = %v, want %v", c.attempts, got, c.want)
		}
	}
}
