package server

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nielsuitterdijk22/quill/internal/httpx"
	"github.com/nielsuitterdijk22/quill/internal/platform"
	"github.com/nielsuitterdijk22/quill/internal/store/db"
)

// This file holds the pipelines (CI) endpoints added in PR 8: listing a
// repository's workflows and runs, viewing a run's job/step tree with logs,
// triggering a run manually, and the Forgejo webhook receiver that auto-triggers
// runs on push / pull_request.

// ---- response shapes -------------------------------------------------------

// pipelineSummaryResponse is a workflow file plus its latest run status.
type pipelineSummaryResponse struct {
	WorkflowPath string       `json:"workflowPath"`
	Name         string       `json:"name"`
	LastRun      *runResponse `json:"lastRun,omitempty"`
}

// runResponse is the public JSON shape for a pipeline run.
type runResponse struct {
	ID           string     `json:"id"`
	RunNumber    int64      `json:"runNumber"`
	WorkflowPath string     `json:"workflowPath,omitempty"`
	Status       string     `json:"status"`
	Event        string     `json:"event"`
	Ref          string     `json:"ref"`
	CommitSha    string     `json:"commitSha"`
	StartedAt    *time.Time `json:"startedAt,omitempty"`
	FinishedAt   *time.Time `json:"finishedAt,omitempty"`
	CreatedAt    time.Time  `json:"createdAt"`
}

func newRunResponse(r db.PipelineRun) runResponse {
	return runResponse{
		ID:         r.ID.String(),
		RunNumber:  r.RunNumber,
		Status:     r.Status,
		Event:      r.Event,
		Ref:        r.Ref,
		CommitSha:  r.CommitSha,
		StartedAt:  tsPtr(r.StartedAt),
		FinishedAt: tsPtr(r.FinishedAt),
		CreatedAt:  r.CreatedAt,
	}
}

// stepResponse is the public JSON shape for a step, including its logs.
type stepResponse struct {
	Name       string     `json:"name"`
	Type       string     `json:"type"`
	Status     string     `json:"status"`
	Logs       string     `json:"logs"`
	StartedAt  *time.Time `json:"startedAt,omitempty"`
	FinishedAt *time.Time `json:"finishedAt,omitempty"`
}

// jobResponse is the public JSON shape for a job and its steps.
type jobResponse struct {
	Key        string         `json:"key"`
	Name       string         `json:"name"`
	RunsOn     string         `json:"runsOn"`
	Status     string         `json:"status"`
	StartedAt  *time.Time     `json:"startedAt,omitempty"`
	FinishedAt *time.Time     `json:"finishedAt,omitempty"`
	Steps      []stepResponse `json:"steps"`
}

// runDetailResponse is a run with its full job/step tree.
type runDetailResponse struct {
	runResponse
	Jobs []jobResponse `json:"jobs"`
}

func newRunDetailResponse(d platform.RunDetail) runDetailResponse {
	out := runDetailResponse{runResponse: newRunResponse(d.Run)}
	out.WorkflowPath = d.Pipeline.WorkflowPath
	out.Jobs = make([]jobResponse, 0, len(d.Jobs))
	for _, jd := range d.Jobs {
		jr := jobResponse{
			Key:        jd.Job.JobKey,
			Name:       jd.Job.Name,
			RunsOn:     jd.Job.RunsOn,
			Status:     jd.Job.Status,
			StartedAt:  tsPtr(jd.Job.StartedAt),
			FinishedAt: tsPtr(jd.Job.FinishedAt),
			Steps:      make([]stepResponse, 0, len(jd.Steps)),
		}
		for _, st := range jd.Steps {
			jr.Steps = append(jr.Steps, stepResponse{
				Name:       st.Name,
				Type:       st.StepType,
				Status:     st.Status,
				Logs:       st.Logs,
				StartedAt:  tsPtr(st.StartedAt),
				FinishedAt: tsPtr(st.FinishedAt),
			})
		}
		out.Jobs = append(out.Jobs, jr)
	}
	return out
}

// tsPtr converts a pgtype.Timestamptz to a *time.Time (nil when SQL NULL).
func tsPtr(ts pgtype.Timestamptz) *time.Time {
	if !ts.Valid {
		return nil
	}
	t := ts.Time
	return &t
}

// ---- handlers --------------------------------------------------------------

// handleListPipelines returns a repository's workflows with their latest run.
func (s *Server) handleListPipelines(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	repo, pipelines, err := s.platform.ListPipelines(r.Context(), actor, chi.URLParam(r, "slug"), chi.URLParam(r, "repo"))
	if err != nil {
		s.writePlatformError(w, err, "could not list pipelines")
		return
	}
	out := make([]pipelineSummaryResponse, 0, len(pipelines))
	for _, p := range pipelines {
		entry := pipelineSummaryResponse{WorkflowPath: p.WorkflowPath, Name: p.Name}
		if p.LastRun != nil {
			lr := newRunResponse(*p.LastRun)
			lr.WorkflowPath = p.WorkflowPath
			entry.LastRun = &lr
		}
		out = append(out, entry)
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"repository": newRepoResponse(repo),
		"pipelines":  out,
	})
}

// handleListRuns returns a repository's most recent runs across all pipelines.
func (s *Server) handleListRuns(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	repo, runs, err := s.platform.ListRuns(r.Context(), actor, chi.URLParam(r, "slug"), chi.URLParam(r, "repo"))
	if err != nil {
		s.writePlatformError(w, err, "could not list runs")
		return
	}
	out := make([]runResponse, 0, len(runs))
	for _, run := range runs {
		out = append(out, newRunResponse(run))
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"repository": newRepoResponse(repo),
		"runs":       out,
	})
}

// handleGetRun returns a single run with its job/step tree. The workflow path is
// supplied as a ?workflow= query value (it may contain slashes).
func (s *Server) handleGetRun(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	workflow := strings.TrimSpace(r.URL.Query().Get("workflow"))
	if workflow == "" {
		httpx.Error(w, http.StatusBadRequest, "invalid_input", "a workflow query parameter is required")
		return
	}
	number, err := strconv.Atoi(chi.URLParam(r, "number"))
	if err != nil || number <= 0 {
		httpx.Error(w, http.StatusBadRequest, "invalid_input", "run number must be a positive integer")
		return
	}
	repo, detail, err := s.platform.GetRun(r.Context(), actor, chi.URLParam(r, "slug"), chi.URLParam(r, "repo"), workflow, number)
	if err != nil {
		s.writePlatformError(w, err, "could not load run")
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"repository": newRepoResponse(repo),
		"run":        newRunDetailResponse(detail),
	})
}

type triggerRunRequest struct {
	Workflow string `json:"workflow"`
	Ref      string `json:"ref"`
}

// handleTriggerRun runs a workflow manually.
func (s *Server) handleTriggerRun(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	var req triggerRunRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	_, run, err := s.platform.TriggerRun(r.Context(), actor, chi.URLParam(r, "slug"), chi.URLParam(r, "repo"), platform.TriggerInput{
		WorkflowPath: req.Workflow,
		Ref:          req.Ref,
		Event:        "manual",
	})
	if err != nil {
		s.writePlatformError(w, err, "could not trigger run")
		return
	}
	out := newRunResponse(run)
	out.WorkflowPath = req.Workflow
	httpx.JSON(w, http.StatusCreated, map[string]any{"run": out})
}

// handleCancelRun transitions a running/queued run to cancelled. 409 if already
// finished.
func (s *Server) handleCancelRun(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	number, err := strconv.Atoi(chi.URLParam(r, "number"))
	if err != nil || number <= 0 {
		httpx.Error(w, http.StatusBadRequest, "invalid_input", "run number must be a positive integer")
		return
	}
	if err := s.platform.CancelRun(r.Context(), actor, chi.URLParam(r, "slug"), chi.URLParam(r, "repo"), number); err != nil {
		s.writePlatformError(w, err, "could not cancel run")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---- webhook receiver ------------------------------------------------------

// forgejoPushPayload is the subset of Forgejo's push/pull_request webhook bodies
// Quill reads to auto-trigger pipelines.
type forgejoPushPayload struct {
	Ref        string `json:"ref"`
	Repository struct {
		Name  string `json:"name"`
		Owner struct {
			Login    string `json:"login"`
			Username string `json:"username"`
		} `json:"owner"`
	} `json:"repository"`
	PullRequest *struct {
		Head struct {
			Ref string `json:"ref"`
		} `json:"head"`
	} `json:"pull_request"`
}

// maxWebhookBody caps the inbound webhook body Quill will read.
const maxWebhookBody = 1 << 20 // 1 MiB

// handleWebhook receives a Forgejo push/pull_request webhook and triggers the
// repository's pipelines. It is unauthenticated by JWT; instead it verifies the
// HMAC signature in X-Forgejo-Signature against QUILL_WEBHOOK_SECRET (skipped in
// dev when the secret is unset).
func (s *Server) handleWebhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxWebhookBody))
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid_input", "could not read request body")
		return
	}
	if !s.verifyWebhookSignature(r, body) {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "invalid webhook signature")
		return
	}

	event := r.Header.Get("X-Forgejo-Event")
	if event == "" {
		event = r.Header.Get("X-Gitea-Event")
	}
	// Only push and pull_request events trigger runs.
	if event != "push" && event != "pull_request" {
		httpx.JSON(w, http.StatusOK, map[string]any{"ignored": true, "event": event})
		return
	}

	var payload forgejoPushPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid_input", "could not parse webhook payload")
		return
	}
	owner := payload.Repository.Owner.Username
	if owner == "" {
		owner = payload.Repository.Owner.Login
	}
	name := payload.Repository.Name
	if owner == "" || name == "" {
		httpx.Error(w, http.StatusBadRequest, "invalid_input", "webhook payload missing repository")
		return
	}
	ref := payload.Ref
	if event == "pull_request" && payload.PullRequest != nil {
		ref = payload.PullRequest.Head.Ref
	}
	ref = strings.TrimPrefix(ref, "refs/heads/")

	runs, err := s.platform.HandleWebhookEvent(r.Context(), owner, name, event, ref)
	if err != nil {
		// A repository Quill doesn't track is not an error worth a 5xx; ack it.
		s.logger.Warn("webhook event could not be handled", "owner", owner, "repo", name, "event", event, "error", err)
		httpx.JSON(w, http.StatusOK, map[string]any{"triggered": 0})
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"triggered": len(runs)})
}

// verifyWebhookSignature checks the HMAC-SHA256 signature Forgejo sends in
// X-Forgejo-Signature (hex of HMAC(secret, body)). It returns true when the
// secret is unset (dev mode) so local development needs no signing.
func (s *Server) verifyWebhookSignature(r *http.Request, body []byte) bool {
	secret := s.cfg.WebhookSecret
	if secret == "" {
		return true
	}
	sig := r.Header.Get("X-Forgejo-Signature")
	if sig == "" {
		sig = r.Header.Get("X-Gitea-Signature")
	}
	if sig == "" {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(expected), []byte(sig))
}
