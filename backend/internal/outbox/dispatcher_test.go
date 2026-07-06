package outbox

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

// clock is a manually-advanced time source shared by the dispatcher and the fake
// store so backoff scheduling can be exercised deterministically.
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

type fakeRow struct {
	id            uuid.UUID
	payload       []byte
	attempts      int32
	nextAttemptAt time.Time
	delivered     bool
}

// fakeStore is an in-memory outbox.Store so the engine's tests need no database.
type fakeStore struct {
	mu    sync.Mutex
	rows  []*fakeRow
	clock *clock
}

func (f *fakeStore) enqueue(payload []byte) uuid.UUID {
	f.mu.Lock()
	defer f.mu.Unlock()
	id := uuid.New()
	f.rows = append(f.rows, &fakeRow{id: id, payload: payload, nextAttemptAt: f.clock.now()})
	return id
}

func (f *fakeStore) ListPending(_ context.Context, limit int32) ([]PendingEvent, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	now := f.clock.now()
	var out []PendingEvent
	for _, r := range f.rows {
		if r.delivered || r.nextAttemptAt.After(now) {
			continue
		}
		out = append(out, PendingEvent{ID: r.id, Payload: r.payload, Attempts: r.attempts})
		if int32(len(out)) >= limit {
			break
		}
	}
	return out, nil
}

func (f *fakeStore) MarkDelivered(_ context.Context, id uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, r := range f.rows {
		if r.id == id {
			r.delivered = true
		}
	}
	return nil
}

func (f *fakeStore) MarkFailed(_ context.Context, id uuid.UUID, nextAttemptAt time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, r := range f.rows {
		if r.id == id {
			r.attempts++
			r.nextAttemptAt = nextAttemptAt
		}
	}
	return nil
}

func (f *fakeStore) get(id uuid.UUID) *fakeRow {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, r := range f.rows {
		if r.id == id {
			cp := *r
			return &cp
		}
	}
	return nil
}

// testDispatcher wires a dispatcher against the fake store, injecting the shared
// clock so backoff scheduling is deterministic (same package, so it can set the
// unexported now hook).
func testDispatcher(url, token string, store *fakeStore) *Dispatcher {
	d := NewDispatcher(Config{
		URL:         url,
		BaseBackoff: time.Minute,
		MaxBackoff:  time.Hour,
	}, store, StaticTokenSource(token), nil)
	d.now = store.clock.now
	return d
}

func TestDispatcherDeliversOnSuccessWithAuth(t *testing.T) {
	var (
		mu       sync.Mutex
		requests int
		gotAuth  string
		gotBody  []byte
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests++
		gotAuth = r.Header.Get("Authorization")
		buf := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(buf)
		gotBody = buf
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	store := &fakeStore{clock: newClock()}
	id := store.enqueue([]byte(`{"hello":"world"}`))
	d := testDispatcher(srv.URL, "secret-token", store)

	n, err := d.ProcessBatch(context.Background())
	if err != nil || n != 1 {
		t.Fatalf("ProcessBatch: n=%d err=%v (want 1, nil)", n, err)
	}
	if row := store.get(id); row == nil || !row.delivered {
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
	if string(gotBody) != `{"hello":"world"}` {
		t.Fatalf("payload delivered verbatim expected, got %q", gotBody)
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

	store := &fakeStore{clock: newClock()}
	id := store.enqueue([]byte(`{}`))
	d := testDispatcher(srv.URL, "", store)

	// First pass: Tempo returns 500 -> failed, retry scheduled in the future.
	if n, err := d.ProcessBatch(context.Background()); err != nil || n != 0 {
		t.Fatalf("first pass: n=%d err=%v (want 0, nil)", n, err)
	}
	if row := store.get(id); row.delivered || row.attempts != 1 {
		t.Fatalf("after a 500 expected undelivered attempts=1, got %+v", row)
	}
	// Not yet due: another pass at the same instant delivers nothing.
	if n, _ := d.ProcessBatch(context.Background()); n != 0 {
		t.Fatalf("event should not be due yet, delivered %d", n)
	}

	// Time passes and Tempo recovers.
	store.clock.advance(2 * time.Minute)
	mu.Lock()
	fail = false
	mu.Unlock()

	if n, err := d.ProcessBatch(context.Background()); err != nil || n != 1 {
		t.Fatalf("retry pass: n=%d err=%v (want 1, nil)", n, err)
	}
	if row := store.get(id); !row.delivered {
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

	store := &fakeStore{clock: newClock()}
	id := store.enqueue([]byte(`{}`))
	d := testDispatcher(url, "", store)

	n, err := d.ProcessBatch(context.Background())
	if err != nil {
		t.Fatalf("ProcessBatch should not error on transport failure: %v", err)
	}
	if n != 0 {
		t.Fatalf("expected 0 delivered while Tempo is down, got %d", n)
	}
	row := store.get(id)
	if row == nil || row.delivered || row.attempts != 1 {
		t.Fatalf("event must survive downtime, undelivered, attempts=1; got %+v", row)
	}
	if !row.nextAttemptAt.After(store.clock.now()) {
		t.Fatal("a retry should be scheduled in the future")
	}
}

func TestDispatcherDisabledWhenURLEmpty(t *testing.T) {
	store := &fakeStore{clock: newClock()}
	store.enqueue([]byte(`{}`))
	d := NewDispatcher(Config{URL: ""}, store, nil, nil)

	if d.Enabled() {
		t.Fatal("dispatcher must be disabled when URL is empty")
	}
	// Run must return promptly (idle) rather than block or poll.
	done := make(chan struct{})
	go func() { d.Run(context.Background()); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run should return immediately when the outbox is disabled")
	}
	for _, r := range store.rows {
		if r.delivered {
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
