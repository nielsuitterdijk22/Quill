package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nielsuitterdijk22/quill/internal/store/db"
	"github.com/nielsuitterdijk22/quill/internal/workertoken"
)

func postToken(t *testing.T, srv *Server, token, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	return rec
}

// TestDispatchWorkerToken: an isolated CI instance authenticates with its
// own worker token. Forged/expired tokens are refused; the worker id always
// comes from the token (a body naming another worker is ignored), so one
// instance can never heartbeat, log to or report another instance's claim.
func TestDispatchWorkerToken(t *testing.T) {
	srv, st := newDispatchTestServer(t)
	ctx := context.Background()
	secret := "test-dispatch-secret"
	mint := func(worker string, exp time.Duration) string {
		tok, err := workertoken.Mint(secret, workertoken.Claims{Worker: worker, Instance: "inst-" + worker, Expires: time.Now().Add(exp).Unix()})
		if err != nil {
			t.Fatal(err)
		}
		return tok
	}

	forged, _ := workertoken.Mint("not-the-secret", workertoken.Claims{Worker: "w", Expires: time.Now().Add(time.Hour).Unix()})
	for name, tok := range map[string]string{"forged": forged, "expired": mint("w-old", -time.Minute)} {
		if rec := postToken(t, srv, tok, "/api/v1/internal/dispatch/claim", map[string]string{"worker": "x"}); rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s token claim: %d", name, rec.Code)
		}
	}
	// The retire endpoint takes worker tokens only.
	if rec := postDispatch(t, srv, secret, "/api/v1/internal/dispatch/retire", map[string]string{}); rec.Code != http.StatusUnauthorized {
		t.Fatalf("HMAC retire: %d", rec.Code)
	}

	// Drain other tests' leftovers so the claim below is ours.
	for i := 0; i < 50; i++ {
		if rec := postDispatch(t, srv, secret, "/api/v1/internal/dispatch/claim", map[string]string{"worker": "drain"}); rec.Code != http.StatusOK {
			break
		}
	}
	tenant, err := st.GetOrCreateTenantByExternalOrg(ctx, "dispatch-token-test-"+uuid.NewString(), "dispatch-token-test")
	if err != nil {
		t.Fatal(err)
	}
	project, err := st.CreateProject(ctx, db.CreateProjectParams{TenantID: tenant.ID, Slug: "dtok-" + uuid.NewString()[:8], Name: "dtok"})
	if err != nil {
		t.Fatal(err)
	}
	repo, err := st.CreateRepository(ctx, db.CreateRepositoryParams{ProjectID: project.ID, Slug: "dtok", Name: "dtok", Visibility: "private", DefaultBranch: "main"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = st.Pool().Exec(ctx, "DELETE FROM repositories WHERE id = $1", repo.ID)
		_, _ = st.Pool().Exec(ctx, "DELETE FROM projects WHERE id = $1", project.ID)
		_, _ = st.Pool().Exec(ctx, "DELETE FROM tenants WHERE id = $1", tenant.ID)
	})
	pl, err := st.UpsertPipeline(ctx, db.UpsertPipelineParams{RepoID: repo.ID, WorkflowPath: ".github/workflows/t.yml", Name: "t"})
	if err != nil {
		t.Fatal(err)
	}
	run, err := st.CreatePipelineRun(ctx, db.CreatePipelineRunParams{PipelineID: pl.ID, RunNumber: 1, Status: "pending", Event: "push", Ref: "main"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.EnqueuePipelineJob(ctx, run.ID, uuid.NullUUID{UUID: tenant.ID, Valid: true}, []byte(`{"WorkflowYAML":"on: [push]"}`)); err != nil {
		t.Fatal(err)
	}

	tokA, tokB := mint("ciw-a-"+uuid.NewString(), time.Hour), mint("ciw-b-"+uuid.NewString(), time.Hour)
	// Worker A claims while pretending, in the body, to be someone else.
	rec := postToken(t, srv, tokA, "/api/v1/internal/dispatch/claim", map[string]string{"worker": "someone-else"})
	if rec.Code != http.StatusOK {
		t.Fatalf("token claim: %d %s", rec.Code, rec.Body.String())
	}
	var claim struct {
		QueueID string `json:"queue_id"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &claim)
	qid, _ := uuid.Parse(claim.QueueID)
	row, err := st.GetPipelineQueueJob(ctx, qid)
	if err != nil {
		t.Fatal(err)
	}
	claimsA, _ := workertoken.Peek(tokA)
	if row.ClaimedBy != claimsA.Worker {
		t.Fatalf("claimed_by = %q, want the token's worker %q", row.ClaimedBy, claimsA.Worker)
	}
	// Worker B cannot touch A's claim, whatever its body says.
	if rec := postToken(t, srv, tokB, "/api/v1/internal/dispatch/heartbeat", map[string]string{"worker": claimsA.Worker, "queue_id": claim.QueueID}); rec.Code != http.StatusConflict {
		t.Fatalf("B heartbeat on A's claim: %d", rec.Code)
	}
	if rec := postToken(t, srv, tokB, "/api/v1/internal/dispatch/report", map[string]any{"worker": claimsA.Worker, "queue_id": claim.QueueID, "error": "hijack"}); rec.Code != http.StatusConflict {
		t.Fatalf("B report on A's claim: %d", rec.Code)
	}
	// A itself can.
	if rec := postToken(t, srv, tokA, "/api/v1/internal/dispatch/heartbeat", map[string]string{"queue_id": claim.QueueID}); rec.Code != http.StatusNoContent {
		t.Fatalf("A heartbeat: %d %s", rec.Code, rec.Body.String())
	}
}
