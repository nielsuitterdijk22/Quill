package workitemrefs

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nielsuitterdijk22/quill/internal/store/db"
)

// The dispatcher's poll/backoff/retry/disabled behavior is exercised once, over
// the shared engine, in internal/outbox. Here we assert the work-item-ref
// adapter delivers the exact wire-contract JSON, and that Enqueue writes or skips
// outbox rows correctly.

// fakeOutbox is a minimal in-memory work_item_ref_outbox.
type fakeOutbox struct {
	mu   sync.Mutex
	rows []*db.WorkItemRefOutbox
}

func (f *fakeOutbox) enqueue(push RefPush) uuid.UUID {
	payload, _ := json.Marshal(push)
	id := uuid.New()
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rows = append(f.rows, &db.WorkItemRefOutbox{ID: id, ProjectID: push.QuillProjectID, Payload: payload})
	return id
}

func (f *fakeOutbox) ListPendingWorkItemRefEvents(_ context.Context, limit int32) ([]db.WorkItemRefOutbox, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []db.WorkItemRefOutbox
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

func (f *fakeOutbox) MarkWorkItemRefEventDelivered(_ context.Context, id uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, r := range f.rows {
		if r.ID == id {
			r.DeliveredAt = pgtype.Timestamptz{Valid: true}
		}
	}
	return nil
}

func (f *fakeOutbox) MarkWorkItemRefEventFailed(_ context.Context, _ db.MarkWorkItemRefEventFailedParams) error {
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
// bearer token attached via the shared TokenSource seam.
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

	out := &fakeOutbox{}
	id := out.enqueue(samplePush())
	d := NewDispatcher(Config{URL: srv.URL}, out, StaticTokenSource("machine-token"), nil)

	n, err := d.ProcessBatch(context.Background())
	if err != nil || n != 1 {
		t.Fatalf("ProcessBatch: n=%d err=%v (want 1, nil)", n, err)
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
