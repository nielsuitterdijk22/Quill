package server

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/google/uuid"

	"github.com/nielsuitterdijk22/quill/internal/httpx"
	"github.com/nielsuitterdijk22/quill/internal/pipeline"
	"github.com/nielsuitterdijk22/quill/internal/platform"
)

// Internal dispatch API (serverless epic, PR A1): the worker-facing surface
// of the pipeline execution queue, authenticated with the same
// QUILL_PIPELINE_DISPATCH_SECRET used for API -> dispatcher requests and the
// same scheme (X-Quill-Dispatch-Signature = HMAC-SHA256 of the raw body, hex)
// — one shared secret between the pieces that move CI work around.
//
//	POST /api/v1/internal/dispatch/claim     {worker}                          -> 200 {queue_id, run_id, spec} | 204
//	POST /api/v1/internal/dispatch/heartbeat {worker, queue_id}               -> 204 | 409
//	POST /api/v1/internal/dispatch/report    {worker, queue_id, result|error} -> 204 | 409
//
// The claim response embeds the full JobSpec (resolved secrets included): the
// caller must hold the dispatch secret, and the payload must never be proxied
// to untrusted consumers.

// verifyDispatch authenticates a request by its X-Quill-Dispatch-Signature
// header against the configured secret and returns the verified body. An
// unset secret refuses every request: the endpoints are unusable unless the
// deployment opted into the dispatch trust group.
func (s *Server) verifyDispatch(r *http.Request) ([]byte, bool) {
	secret := s.cfg.Pipeline.DispatchSecret
	if secret == "" {
		return nil, false
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 16<<20))
	if err != nil {
		return nil, false
	}
	want := dispatchSignature(secret, body)
	got := r.Header.Get("X-Quill-Dispatch-Signature")
	if got == "" || !hmac.Equal([]byte(want), []byte(got)) {
		return nil, false
	}
	return body, true
}

// dispatchSignature returns the hex HMAC-SHA256 of body under secret.
func dispatchSignature(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

type dispatchClaimBody struct {
	Worker string `json:"worker"`
}

type dispatchClaimResponse struct {
	QueueID  string           `json:"queue_id"`
	RunID    string           `json:"run_id"`
	Attempts int32            `json:"attempts"`
	TenantID string           `json:"tenant_id,omitempty"`
	Spec     pipeline.JobSpec `json:"spec"`
}

type dispatchHeartbeatBody struct {
	Worker  string `json:"worker"`
	QueueID string `json:"queue_id"`
}

type dispatchReportBody struct {
	Worker  string              `json:"worker"`
	QueueID string              `json:"queue_id"`
	Error   string              `json:"error,omitempty"`
	Result  *pipeline.RunResult `json:"result,omitempty"`
}

// handleDispatchClaim hands the next claimable job to a worker.
func (s *Server) handleDispatchClaim(w http.ResponseWriter, r *http.Request) {
	body, ok := s.verifyDispatch(r)
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "invalid or missing dispatch signature")
		return
	}
	var in dispatchClaimBody
	if err := json.Unmarshal(body, &in); err != nil || in.Worker == "" {
		httpx.Error(w, http.StatusBadRequest, "invalid_request", "a worker id is required")
		return
	}
	job, err := s.platform.ClaimQueuedJob(r.Context(), in.Worker)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "claim_failed", err.Error())
		return
	}
	if job == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	resp := dispatchClaimResponse{
		QueueID:  job.QueueID.String(),
		RunID:    job.RunID.String(),
		Attempts: job.Attempts,
		Spec:     job.Spec,
	}
	if job.TenantID.Valid {
		resp.TenantID = job.TenantID.UUID.String()
	}
	httpx.JSON(w, http.StatusOK, resp)
}

// handleDispatchHeartbeat refreshes a claim so the visibility timeout does
// not reclaim a long-running job. 409 means the job changed hands: the worker
// must abandon the run.
func (s *Server) handleDispatchHeartbeat(w http.ResponseWriter, r *http.Request) {
	body, ok := s.verifyDispatch(r)
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "invalid or missing dispatch signature")
		return
	}
	var in dispatchHeartbeatBody
	if err := json.Unmarshal(body, &in); err != nil || in.Worker == "" || in.QueueID == "" {
		httpx.Error(w, http.StatusBadRequest, "invalid_request", "worker and queue_id are required")
		return
	}
	queueID, err := uuid.Parse(in.QueueID)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid_request", "queue_id is not a uuid")
		return
	}
	alive, err := s.platform.HeartbeatQueuedJob(r.Context(), in.Worker, queueID)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "heartbeat_failed", err.Error())
		return
	}
	if !alive {
		httpx.Error(w, http.StatusConflict, "claim_lost", "the job was reclaimed by another worker")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleDispatchReport records a worker's run outcome (result or execution
// error) and finishes the queue row.
func (s *Server) handleDispatchReport(w http.ResponseWriter, r *http.Request) {
	body, ok := s.verifyDispatch(r)
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "invalid or missing dispatch signature")
		return
	}
	var in dispatchReportBody
	if err := json.Unmarshal(body, &in); err != nil || in.Worker == "" || in.QueueID == "" {
		httpx.Error(w, http.StatusBadRequest, "invalid_request", "worker and queue_id are required")
		return
	}
	queueID, err := uuid.Parse(in.QueueID)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid_request", "queue_id is not a uuid")
		return
	}
	if in.Error == "" && in.Result == nil {
		httpx.Error(w, http.StatusBadRequest, "invalid_request", "a report carries either result or error")
		return
	}
	if err := s.platform.ReportQueuedJob(r.Context(), in.Worker, queueID, in.Result, in.Error); err != nil {
		switch {
		case errors.Is(err, platform.ErrNotFound):
			httpx.Error(w, http.StatusNotFound, "not_found", "unknown queue job")
		case errors.Is(err, platform.ErrConflict):
			httpx.Error(w, http.StatusConflict, "claim_lost", "the job is no longer yours to report")
		default:
			httpx.Error(w, http.StatusInternalServerError, "report_failed", err.Error())
		}
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type dispatchLogsBody struct {
	Worker  string              `json:"worker"`
	QueueID string              `json:"queue_id"`
	Lines   []platform.LiveLine `json:"lines"`
}

// handleDispatchLogs stores a batch of live output lines for a claimed job
// (POST /internal/dispatch/logs). 409 means the claim is no longer the
// worker's.
func (s *Server) handleDispatchLogs(w http.ResponseWriter, r *http.Request) {
	body, ok := s.verifyDispatch(r)
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "invalid or missing dispatch signature")
		return
	}
	var in dispatchLogsBody
	if err := json.Unmarshal(body, &in); err != nil || in.Worker == "" || in.QueueID == "" {
		httpx.Error(w, http.StatusBadRequest, "invalid_request", "worker and queue_id are required")
		return
	}
	queueID, err := uuid.Parse(in.QueueID)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid_request", "queue_id is not a uuid")
		return
	}
	if err := s.platform.AppendQueuedJobLogs(r.Context(), in.Worker, queueID, in.Lines); err != nil {
		switch {
		case errors.Is(err, platform.ErrNotFound):
			httpx.Error(w, http.StatusNotFound, "not_found", "unknown queue job")
		case errors.Is(err, platform.ErrConflict):
			httpx.Error(w, http.StatusConflict, "claim_lost", "the job is no longer yours")
		case errors.Is(err, platform.ErrInvalidInput):
			httpx.Error(w, http.StatusBadRequest, "invalid_request", err.Error())
		default:
			httpx.Error(w, http.StatusInternalServerError, "logs_failed", err.Error())
		}
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
