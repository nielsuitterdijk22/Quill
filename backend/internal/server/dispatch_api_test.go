package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/nielsuitterdijk22/quill/internal/config"
	"github.com/nielsuitterdijk22/quill/internal/logging"
	"github.com/nielsuitterdijk22/quill/internal/store"
	"github.com/nielsuitterdijk22/quill/internal/store/db"
)

// newDispatchTestServer builds a server with the pipeline dispatch secret set
// and queue mode on, against the gated test database. Skips without
// QUILL_TEST_DATABASE_URL like the platform integration tests.
func newDispatchTestServer(t *testing.T) (*Server, *store.Store) {
	t.Helper()
	dsn := os.Getenv("QUILL_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("QUILL_TEST_DATABASE_URL not set; skipping dispatch API integration test")
	}
	if err := store.Migrate(dsn); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	st, err := store.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(st.Close)
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	cfg.Pipeline.DispatchSecret = "test-dispatch-secret"
	cfg.Pipeline.QueueMode = true
	return New(cfg, logging.New("error", "text"), st), st
}

// postDispatch signs a request to the internal dispatch endpoints.
func postDispatch(t *testing.T, srv *Server, secret, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Quill-Dispatch-Signature", dispatchSignature(secret, raw))
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	return rec
}

func TestDispatchClaimRequiresValidSignature(t *testing.T) {
	srv, _ := newDispatchTestServer(t)

	// Unsigned.
	req := httptest.NewRequest(http.MethodPost, "/api/v1/internal/dispatch/claim",
		strings.NewReader(`{"worker":"w1"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unsigned claim: expected 401, got %d", rec.Code)
	}

	// Wrong secret.
	rec = postDispatch(t, srv, "wrong-secret", "/api/v1/internal/dispatch/claim",
		map[string]string{"worker": "w1"})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("bad-signature claim: expected 401, got %d", rec.Code)
	}

	// Valid signature, empty queue: 204 No Content.
	rec = postDispatch(t, srv, "test-dispatch-secret", "/api/v1/internal/dispatch/claim",
		map[string]string{"worker": "w1"})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("empty-queue claim: expected 204, got %d (body %s)", rec.Code, rec.Body.String())
	}
}

// TestDispatchHTTPRoundTrip drives the whole worker protocol over HTTP: seed a
// queued run through the store, claim it, heartbeat it, and report a success
// result — then verify the run row reflects the outcome.
func TestDispatchHTTPRoundTrip(t *testing.T) {
	srv, st := newDispatchTestServer(t)
	ctx := context.Background()

	tenant, err := st.GetOrCreateTenantByExternalOrg(ctx, "dispatch-http-test", "dispatch-http-test")
	if err != nil {
		t.Fatalf("tenant: %v", err)
	}
	project, err := st.CreateProject(ctx, db.CreateProjectParams{
		TenantID: tenant.ID, Slug: "dispatchapi", Name: "dispatchapi",
	})
	if err != nil {
		t.Fatalf("project: %v", err)
	}
	repo, err := st.CreateRepository(ctx, db.CreateRepositoryParams{
		ProjectID: project.ID, Slug: "dispatchapi", Name: "dispatchapi",
		Visibility: "private", DefaultBranch: "main",
	})
	// Leave nothing behind: leftover projects RESTRICT tenant deletion and
	// then abort other packages' whole-table test resets mid-statement.
	t.Cleanup(func() {
		_, _ = st.Pool().Exec(ctx, "DELETE FROM repositories WHERE id = $1", repo.ID)
		_, _ = st.Pool().Exec(ctx, "DELETE FROM projects WHERE id = $1", project.ID)
		_, _ = st.Pool().Exec(ctx, "DELETE FROM tenants WHERE id = $1", tenant.ID)
	})
	if err != nil {
		t.Fatalf("repo: %v", err)
	}
	pl, err := st.UpsertPipeline(ctx, db.UpsertPipelineParams{
		RepoID: repo.ID, WorkflowPath: ".github/workflows/test.yml", Name: "test",
	})
	if err != nil {
		t.Fatalf("pipeline: %v", err)
	}
	run, err := st.CreatePipelineRun(ctx, db.CreatePipelineRunParams{
		PipelineID: pl.ID, RunNumber: 1, Status: "pending", Event: "push", Ref: "main",
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	spec := map[string]any{
		"workflow_yaml":  `name: test` + "\n" + `on: [push]`,
		"ref":            "main",
		"event":          "push",
		"repo_full_name": "dispatchapi/dispatchapi",
	}
	raw, err := json.Marshal(spec)
	if err != nil {
		t.Fatalf("marshal spec: %v", err)
	}
	if err := st.EnqueuePipelineJob(ctx, run.ID, uuid.NullUUID{UUID: tenant.ID, Valid: true}, raw); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	// Claim: the response carries identifiers and the decoded spec.
	rec := postDispatch(t, srv, "test-dispatch-secret", "/api/v1/internal/dispatch/claim",
		map[string]string{"worker": "w1"})
	if rec.Code != http.StatusOK {
		t.Fatalf("claim: expected 200, got %d (body %s)", rec.Code, rec.Body.String())
	}
	var claimed struct {
		QueueID  string `json:"queue_id"`
		RunID    string `json:"run_id"`
		Attempts int32  `json:"attempts"`
		TenantID string `json:"tenant_id"`
		Spec     struct {
			WorkflowYAML string `json:"WorkflowYAML"`
			Ref          string `json:"Ref"`
		} `json:"spec"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &claimed); err != nil {
		t.Fatalf("decode claim: %v", err)
	}
	if claimed.QueueID == "" || claimed.RunID != run.ID.String() || claimed.Attempts != 1 {
		t.Fatalf("claim payload mismatch: %+v", claimed)
	}
	if claimed.TenantID != tenant.ID.String() {
		t.Fatalf("claim tenant = %q, want %q", claimed.TenantID, tenant.ID)
	}

	// Heartbeat keeps the claim; a different worker's heartbeat is refused.
	rec = postDispatch(t, srv, "test-dispatch-secret", "/api/v1/internal/dispatch/heartbeat",
		map[string]string{"worker": "w1", "queue_id": claimed.QueueID})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("heartbeat: expected 204, got %d (body %s)", rec.Code, rec.Body.String())
	}
	rec = postDispatch(t, srv, "test-dispatch-secret", "/api/v1/internal/dispatch/heartbeat",
		map[string]string{"worker": "w2", "queue_id": claimed.QueueID})
	if rec.Code != http.StatusConflict {
		t.Fatalf("foreign heartbeat: expected 409, got %d", rec.Code)
	}

	// Report success; the run flips to success and the queue row is done.
	rec = postDispatch(t, srv, "test-dispatch-secret", "/api/v1/internal/dispatch/report",
		map[string]any{
			"worker": "w1", "queue_id": claimed.QueueID,
			"result": map[string]any{"Status": "success", "Jobs": []any{}},
		})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("report: expected 204, got %d (body %s)", rec.Code, rec.Body.String())
	}
	got, err := st.GetPipelineRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("reload run: %v", err)
	}
	if got.Status != "success" {
		t.Fatalf("reported run status = %q, want success", got.Status)
	}

	// A stale worker reporting again is refused: the queue row is done.
	rec = postDispatch(t, srv, "test-dispatch-secret", "/api/v1/internal/dispatch/report",
		map[string]any{
			"worker": "w1", "queue_id": claimed.QueueID,
			"result": map[string]any{"Status": "failure", "Jobs": []any{}},
		})
	if rec.Code != http.StatusConflict {
		t.Fatalf("double report: expected 409, got %d", rec.Code)
	}
}
