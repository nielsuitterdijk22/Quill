package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nielsuitterdijk22/quill/internal/config"
	"github.com/nielsuitterdijk22/quill/internal/logging"
	"github.com/nielsuitterdijk22/quill/internal/store"
	"github.com/nielsuitterdijk22/quill/internal/store/db"
	"github.com/nielsuitterdijk22/quill/internal/workitemrefs"
)

// These tests exercise the Forgejo webhook -> work-item-ref outbox path against
// a live Postgres, following the same convention as the store/platform
// integration tests: skipped unless QUILL_TEST_DATABASE_URL is set so
// `go test ./...` stays green without a DB.
func refsTestStore(t *testing.T) *store.Store {
	t.Helper()
	dsn := os.Getenv("QUILL_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("QUILL_TEST_DATABASE_URL not set; skipping webhook refs integration test")
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
		// work_item_ref_outbox has no FK to projects (by design), so truncate it
		// explicitly alongside the cascading project/repo truncate.
		_, _ = st.Pool().Exec(context.Background(), "TRUNCATE projects, work_item_ref_outbox CASCADE")
	}
	reset()
	t.Cleanup(reset)
	return st
}

// seedRepo creates a project ("acme") and repository ("web") that the webhook's
// owner/name resolution maps to (no Forgejo link -> owner = project slug,
// name = repo slug).
func seedRepo(t *testing.T, st *store.Store) (db.Project, db.Repository) {
	t.Helper()
	ctx := context.Background()
	tenant, err := st.GetTenantBySlug(ctx, "default")
	if err != nil {
		t.Fatalf("get default tenant: %v", err)
	}
	project, err := st.CreateProject(ctx, db.CreateProjectParams{
		TenantID: tenant.ID, Slug: "acme", Name: "Acme",
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	repo, err := st.CreateRepository(ctx, db.CreateRepositoryParams{
		ProjectID: project.ID, Slug: "web", Name: "web",
		Visibility: "private", DefaultBranch: "main",
	})
	if err != nil {
		t.Fatalf("create repository: %v", err)
	}
	return project, repo
}

// refsServer builds a Server against st with the refs endpoint set to refsURL
// (empty = feature disabled) and no webhook secret, so test posts need no HMAC.
func refsServer(t *testing.T, st *store.Store, refsURL string) *Server {
	t.Helper()
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	cfg.WebhookSecret = ""
	cfg.TempoSync.RefsURL = refsURL
	return New(cfg, logging.New("error", "text"), st)
}

func postWebhook(t *testing.T, srv *Server, event, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/forgejo", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Forgejo-Event", event)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	return rec
}

const pushBody = `{
	"ref": "refs/heads/ABC-7-fix",
	"repository": {"name": "web", "owner": {"username": "acme"}},
	"commits": [
		{
			"id": "cafebabe1",
			"message": "abc-1 fix login\n\nlonger body mentioning UTF-8",
			"url": "http://forge.example/acme/web/commit/cafebabe1",
			"author": {"name": "Alice", "username": "alice"}
		},
		{
			"id": "cafebabe2",
			"message": "chore: tidy imports",
			"url": "http://forge.example/acme/web/commit/cafebabe2",
			"author": {"name": "Bob", "username": "bob"}
		}
	]
}`

const prBody = `{
	"ref": "refs/heads/main",
	"repository": {"name": "web", "owner": {"username": "acme"}},
	"pull_request": {
		"number": 42,
		"title": "DEF-9: add search",
		"body": "closes abc-100",
		"state": "open",
		"html_url": "http://forge.example/acme/web/pulls/42",
		"user": {"username": "carol"},
		"head": {"ref": "feature/def-9"}
	}
}`

// TestWebhookEnqueuesAndDispatcherDelivers walks the full outbound path: a push
// webhook enqueues an outbox row without ever calling Tempo inline (the response
// must not block on Tempo), then the dispatcher delivers the exact wire-contract
// JSON to a fake Tempo endpoint.
func TestWebhookEnqueuesAndDispatcherDelivers(t *testing.T) {
	st := refsTestStore(t)
	project, _ := seedRepo(t, st)
	ctx := context.Background()

	var (
		mu      sync.Mutex
		hits    int
		gotAuth string
		gotBody map[string]any
	)
	tempo := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits++
		gotAuth = r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer tempo.Close()

	srv := refsServer(t, st, tempo.URL)

	// The webhook must ack promptly and must not touch Tempo synchronously.
	start := time.Now()
	rec := postWebhook(t, srv, "push", pushBody)
	if rec.Code != http.StatusOK {
		t.Fatalf("webhook status = %d, body %s", rec.Code, rec.Body.String())
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("webhook took %v; must not block on Tempo", elapsed)
	}
	mu.Lock()
	if hits != 0 {
		t.Fatalf("Tempo was called %d times during the webhook; delivery must be async", hits)
	}
	mu.Unlock()

	// One outbox row, tagged with the repo's Quill project id.
	rows, err := st.ListPendingWorkItemRefEvents(ctx, 10)
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 pending push, got %d", len(rows))
	}
	if rows[0].ProjectID != project.ID {
		t.Fatalf("outbox project id = %s, want %s", rows[0].ProjectID, project.ID)
	}

	// Deliver via the dispatcher and assert the exact contract body.
	d := workitemrefs.NewDispatcher(workitemrefs.Config{URL: tempo.URL}, st, workitemrefs.StaticTokenSource("machine-token"), nil)
	if n, err := d.ProcessBatch(ctx); err != nil || n != 1 {
		t.Fatalf("ProcessBatch: n=%d err=%v (want 1, nil)", n, err)
	}

	mu.Lock()
	defer mu.Unlock()
	if hits != 1 {
		t.Fatalf("expected exactly 1 delivery, got %d", hits)
	}
	if gotAuth != "Bearer machine-token" {
		t.Fatalf("auth = %q, want the machine bearer token", gotAuth)
	}
	if gotBody["quillProjectId"] != project.ID.String() {
		t.Fatalf("quillProjectId = %v, want %s", gotBody["quillProjectId"], project.ID)
	}
	refs, _ := gotBody["refs"].([]any)
	if len(refs) != 2 {
		t.Fatalf("refs = %v, want 2 commit refs (branch key carries the keyless commit)", gotBody["refs"])
	}
	first := refs[0].(map[string]any)
	if first["refType"] != "commit" || first["repoSlug"] != "web" ||
		first["externalRef"] != "cafebabe1" ||
		first["url"] != "http://forge.example/acme/web/commit/cafebabe1" ||
		first["title"] != "abc-1 fix login" || first["state"] != "" ||
		first["author"] != "alice" {
		t.Fatalf("commit ref mismatch: %v", first)
	}
	// Message key uppercased, UTF-8 false positive included (Tempo drops it),
	// branch key appended, all deduped.
	wantKeys := []any{"ABC-1", "UTF-8", "ABC-7"}
	if keys, _ := first["workItemKeys"].([]any); len(keys) != len(wantKeys) ||
		keys[0] != wantKeys[0] || keys[1] != wantKeys[1] || keys[2] != wantKeys[2] {
		t.Fatalf("workItemKeys = %v, want %v", first["workItemKeys"], wantKeys)
	}
	second := refs[1].(map[string]any)
	if second["externalRef"] != "cafebabe2" || second["author"] != "bob" {
		t.Fatalf("second commit ref mismatch: %v", second)
	}
	if keys, _ := second["workItemKeys"].([]any); len(keys) != 1 || keys[0] != "ABC-7" {
		t.Fatalf("second workItemKeys = %v, want [ABC-7] (branch only)", second["workItemKeys"])
	}

	// The row is settled: nothing left pending.
	if rows, err := st.ListPendingWorkItemRefEvents(ctx, 10); err != nil || len(rows) != 0 {
		t.Fatalf("expected empty outbox after delivery, got %d rows (err %v)", len(rows), err)
	}
}

// TestWebhookEnqueuesPullRequestRef asserts a pull_request event produces a
// single "pr" ref built from title/body/state/author and the source branch.
func TestWebhookEnqueuesPullRequestRef(t *testing.T) {
	st := refsTestStore(t)
	project, _ := seedRepo(t, st)
	ctx := context.Background()

	srv := refsServer(t, st, "http://tempo.invalid/api/v1/quill/work-item-refs")
	if rec := postWebhook(t, srv, "pull_request", prBody); rec.Code != http.StatusOK {
		t.Fatalf("webhook status = %d, body %s", rec.Code, rec.Body.String())
	}

	rows, err := st.ListPendingWorkItemRefEvents(ctx, 10)
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 pending push, got %d", len(rows))
	}
	var push workitemrefs.RefPush
	if err := json.Unmarshal(rows[0].Payload, &push); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if push.QuillProjectID != project.ID {
		t.Fatalf("quillProjectId = %s, want %s", push.QuillProjectID, project.ID)
	}
	if len(push.Refs) != 1 {
		t.Fatalf("expected 1 pr ref, got %+v", push.Refs)
	}
	ref := push.Refs[0]
	if ref.RefType != "pr" || ref.RepoSlug != "web" || ref.ExternalRef != "42" ||
		ref.URL != "http://forge.example/acme/web/pulls/42" ||
		ref.Title != "DEF-9: add search" || ref.State != "open" || ref.Author != "carol" {
		t.Fatalf("pr ref mismatch: %+v", ref)
	}
	// Title key, body key uppercased, branch key deduped against the title's.
	if len(ref.WorkItemKeys) != 2 || ref.WorkItemKeys[0] != "DEF-9" || ref.WorkItemKeys[1] != "ABC-100" {
		t.Fatalf("workItemKeys = %v, want [DEF-9 ABC-100]", ref.WorkItemKeys)
	}
}

// TestWebhookNoKeysEnqueuesNothing: a push whose commits and branch mention no
// work item must not write an outbox row at all.
func TestWebhookNoKeysEnqueuesNothing(t *testing.T) {
	st := refsTestStore(t)
	seedRepo(t, st)

	srv := refsServer(t, st, "http://tempo.invalid/api/v1/quill/work-item-refs")
	body := `{
		"ref": "refs/heads/main",
		"repository": {"name": "web", "owner": {"username": "acme"}},
		"commits": [{"id": "aaa", "message": "no keys here", "url": "u", "author": {"username": "a"}}]
	}`
	if rec := postWebhook(t, srv, "push", body); rec.Code != http.StatusOK {
		t.Fatalf("webhook status = %d", rec.Code)
	}
	if rows, err := st.ListPendingWorkItemRefEvents(context.Background(), 10); err != nil || len(rows) != 0 {
		t.Fatalf("expected empty outbox, got %d rows (err %v)", len(rows), err)
	}
}

// TestWebhookRefsDisabledWhenUnconfigured: with QUILL_TEMPO_SYNC_REFS_URL empty
// the feature is off — the webhook still acks normally and nothing is enqueued,
// even when the payload is full of keys.
func TestWebhookRefsDisabledWhenUnconfigured(t *testing.T) {
	st := refsTestStore(t)
	seedRepo(t, st)

	srv := refsServer(t, st, "")
	if rec := postWebhook(t, srv, "push", pushBody); rec.Code != http.StatusOK {
		t.Fatalf("webhook status = %d, body %s", rec.Code, rec.Body.String())
	}
	if rows, err := st.ListPendingWorkItemRefEvents(context.Background(), 10); err != nil || len(rows) != 0 {
		t.Fatalf("disabled feature must enqueue nothing, got %d rows (err %v)", len(rows), err)
	}
}

// TestWebhookRefsNoStore: a Server built without a store (as in the existing
// handler tests) must keep serving webhooks untouched — the refs path is
// entirely inert.
func TestWebhookRefsNoStore(t *testing.T) {
	srv := newTestServer(t)
	if rec := postWebhook(t, srv, "push", pushBody); rec.Code != http.StatusOK {
		t.Fatalf("webhook status = %d, body %s", rec.Code, rec.Body.String())
	}
}
