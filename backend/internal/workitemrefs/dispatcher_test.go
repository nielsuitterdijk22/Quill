package workitemrefs

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

// clock is a manually-advanced time source shared by the dispatcher and the fake
// outbox so backoff scheduling can be exercised deterministically.
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
	rows  []*db.WorkItemRefOutbox
	clock *clock
}

func (f *fakeOutbox) enqueue(push RefPush) uuid.UUID {
	payload, _ := json.Marshal(push)
	id := uuid.New()
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rows = append(f.rows, &db.WorkItemRefOutbox{
		ID:            id,
		ProjectID:     push.QuillProjectID,
		Payload:       payload,
		OccurredAt:    f.clock.now(),
		NextAttemptAt: f.clock.now(),
	})
	return id
}

func (f *fakeOutbox) ListPendingWorkItemRefEvents(_ context.Context, limit int32) ([]db.WorkItemRefOutbox, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	now := f.clock.now()
	var out []db.WorkItemRefOutbox
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

func (f *fakeOutbox) MarkWorkItemRefEventDelivered(_ context.Context, id uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, r := range f.rows {
		if r.ID == id {
			r.DeliveredAt = pgtype.Timestamptz{Time: f.clock.now(), Valid: true}
		}
	}
	return nil
}

func (f *fakeOutbox) MarkWorkItemRefEventFailed(_ context.Context, arg db.MarkWorkItemRefEventFailedParams) error {
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

func (f *fakeOutbox) get(id uuid.UUID) *db.WorkItemRefOutbox {
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

func samplePush() RefPush {
	return RefPush{
		QuillProjectID: uuid.MustParse("11111111-2222-3333-4444-555555555555"),
		Refs: []Ref{
			{
				RefType:      "commit",
				RepoSlug:     "web",
				ExternalRef:  "deadbeef",
				URL:          "https://forge.example/acme/web/commit/deadbeef",
				Title:        "ABC-1 fix",
				State:        "",
				Author:       "alice",
				WorkItemKeys: []string{"ABC-1"},
			},
			{
				RefType:      "pr",
				RepoSlug:     "web",
				ExternalRef:  "42",
				URL:          "https://forge.example/acme/web/pulls/42",
				Title:        "DEF-9: add search",
				State:        "open",
				Author:       "carol",
				WorkItemKeys: []string{"DEF-9", "XY-7"},
			},
		},
	}
}

// TestDispatcherDeliversContractJSON asserts the exact wire contract body reaches
// Tempo: quillProjectId, the refs array, and per-ref workItemKeys, with the
// bearer token attached via the TokenSource seam.
func TestDispatcherDeliversContractJSON(t *testing.T) {
	var (
		mu      sync.Mutex
		hits    int
		gotAuth string
		gotBody map[string]any
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits++
		gotAuth = r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	out := &fakeOutbox{clock: newClock()}
	id := out.enqueue(samplePush())
	d := testDispatcher(t, srv.URL, "machine-token", out)

	n, err := d.ProcessBatch(context.Background())
	if err != nil {
		t.Fatalf("ProcessBatch: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 delivered, got %d", n)
	}
	if row := out.get(id); row == nil || !row.DeliveredAt.Valid {
		t.Fatalf("row should be marked delivered, got %+v", row)
	}

	mu.Lock()
	defer mu.Unlock()
	if hits != 1 {
		t.Fatalf("expected 1 request, got %d", hits)
	}
	if gotAuth != "Bearer machine-token" {
		t.Fatalf("auth header = %q, want Bearer machine-token", gotAuth)
	}
	if gotBody["quillProjectId"] != "11111111-2222-3333-4444-555555555555" {
		t.Fatalf("quillProjectId = %v", gotBody["quillProjectId"])
	}
	refs, ok := gotBody["refs"].([]any)
	if !ok || len(refs) != 2 {
		t.Fatalf("refs = %v, want 2 entries", gotBody["refs"])
	}

	commit := refs[0].(map[string]any)
	if commit["refType"] != "commit" || commit["repoSlug"] != "web" ||
		commit["externalRef"] != "deadbeef" || commit["state"] != "" ||
		commit["author"] != "alice" {
		t.Fatalf("commit ref mismatch: %v", commit)
	}
	if keys, _ := commit["workItemKeys"].([]any); len(keys) != 1 || keys[0] != "ABC-1" {
		t.Fatalf("commit workItemKeys = %v, want [ABC-1]", commit["workItemKeys"])
	}

	pr := refs[1].(map[string]any)
	if pr["refType"] != "pr" || pr["externalRef"] != "42" || pr["state"] != "open" {
		t.Fatalf("pr ref mismatch: %v", pr)
	}
	if keys, _ := pr["workItemKeys"].([]any); len(keys) != 2 || keys[0] != "DEF-9" || keys[1] != "XY-7" {
		t.Fatalf("pr workItemKeys = %v, want [DEF-9 XY-7]", pr["workItemKeys"])
	}
}

// TestDispatcher500ThenSuccess exercises retry/durability: a 5xx reschedules the
// row with backoff (never dropped), and a later pass delivers it once Tempo is up.
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
	id := out.enqueue(samplePush())
	d := testDispatcher(t, srv.URL, "", out)

	if n, err := d.ProcessBatch(context.Background()); err != nil || n != 0 {
		t.Fatalf("first pass: n=%d err=%v (want 0, nil)", n, err)
	}
	row := out.get(id)
	if row.DeliveredAt.Valid {
		t.Fatal("row must not be delivered after a 500")
	}
	if row.Attempts != 1 {
		t.Fatalf("expected attempts=1, got %d", row.Attempts)
	}
	// Not yet due: another pass at the same instant delivers nothing.
	if n, _ := d.ProcessBatch(context.Background()); n != 0 {
		t.Fatalf("row should not be due yet, delivered %d", n)
	}

	out.clock.advance(2 * time.Minute)
	mu.Lock()
	fail = false
	mu.Unlock()

	if n, err := d.ProcessBatch(context.Background()); err != nil || n != 1 {
		t.Fatalf("retry pass: n=%d err=%v (want 1, nil)", n, err)
	}
	if row := out.get(id); !row.DeliveredAt.Valid {
		t.Fatal("row should be delivered after Tempo recovered")
	}
}

func TestDispatcherSurvivesTempoDown(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close() // unreachable

	out := &fakeOutbox{clock: newClock()}
	id := out.enqueue(samplePush())
	d := testDispatcher(t, url, "", out)

	n, err := d.ProcessBatch(context.Background())
	if err != nil {
		t.Fatalf("ProcessBatch should not error on transport failure: %v", err)
	}
	if n != 0 {
		t.Fatalf("expected 0 delivered while Tempo is down, got %d", n)
	}
	row := out.get(id)
	if row == nil || row.DeliveredAt.Valid {
		t.Fatal("row must survive and stay undelivered while Tempo is down")
	}
	if row.Attempts != 1 || !row.NextAttemptAt.After(out.clock.now()) {
		t.Fatalf("a retry should be scheduled in the future: %+v", row)
	}
}

func TestDispatcherDisabledWhenURLEmpty(t *testing.T) {
	out := &fakeOutbox{clock: newClock()}
	out.enqueue(samplePush())
	d := NewDispatcher(Config{URL: ""}, out, nil, nil)

	if d.Enabled() {
		t.Fatal("dispatcher must be disabled when URL is empty")
	}
	done := make(chan struct{})
	go func() { d.Run(context.Background()); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run should return immediately when the feature is disabled")
	}
	for _, r := range out.rows {
		if r.DeliveredAt.Valid {
			t.Fatal("disabled dispatcher must not deliver")
		}
	}
}

// fakeWriter records InsertWorkItemRefEvent calls for the Enqueue unit test.
type fakeWriter struct{ inserts int }

func (f *fakeWriter) InsertWorkItemRefEvent(_ context.Context, arg db.InsertWorkItemRefEventParams) (db.WorkItemRefOutbox, error) {
	f.inserts++
	return db.WorkItemRefOutbox{ID: arg.ID, ProjectID: arg.ProjectID, Payload: arg.Payload}, nil
}

func TestEnqueueSkipsEmptyPush(t *testing.T) {
	w := &fakeWriter{}
	if err := Enqueue(context.Background(), w, RefPush{QuillProjectID: uuid.New()}); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if w.inserts != 0 {
		t.Fatalf("expected no insert for a push with zero refs, got %d", w.inserts)
	}
}

func TestEnqueueWritesRow(t *testing.T) {
	w := &fakeWriter{}
	if err := Enqueue(context.Background(), w, samplePush()); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if w.inserts != 1 {
		t.Fatalf("expected 1 insert, got %d", w.inserts)
	}
}
