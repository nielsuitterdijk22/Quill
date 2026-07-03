// Package dispatch exposes the standalone pipeline dispatcher HTTP service.
package dispatch

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"

	"github.com/nielsuitterdijk22/quill/internal/httpx"
	"github.com/nielsuitterdijk22/quill/internal/pipeline"
)

const maxDispatchBody = 4 << 20 // 4 MiB

// rawSSEEvent is a serialised SSE event ready to write to a client.
type rawSSEEvent struct {
	Event string
	Data  []byte
}

// sseLogPayload is the JSON data for a "log" SSE event.
type sseLogPayload struct {
	JobKey   string `json:"jobKey"`
	StepName string `json:"stepName"`
	Line     string `json:"line"`
}

// sseDonePayload is the JSON data for a "done" SSE event.
// Result is included so the HTTPRunner can reconstruct the RunResult without a
// separate round-trip.
type sseDonePayload struct {
	Status string              `json:"status"`
	Error  string              `json:"error,omitempty"`
	Result *pipeline.RunResult `json:"result,omitempty"`
}

// activeJob tracks live SSE subscribers for a single in-progress dispatch job.
// Buffering all events lets late subscribers replay the full log history.
type activeJob struct {
	mu       sync.Mutex
	subs     map[string]chan rawSSEEvent
	buffered []rawSSEEvent
	done     bool
}

func newActiveJob() *activeJob {
	return &activeJob{subs: make(map[string]chan rawSSEEvent)}
}

// publish fans a new event out to all subscribers and appends it to the buffer.
func (a *activeJob) publish(ev rawSSEEvent) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.buffered = append(a.buffered, ev)
	for _, ch := range a.subs {
		select {
		case ch <- ev:
		default: // slow subscriber; drop rather than block
		}
	}
}

// finish marks the job as done and closes all subscriber channels. It is
// idempotent — safe to call multiple times.
func (a *activeJob) finish(ev rawSSEEvent) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.done {
		return
	}
	a.buffered = append(a.buffered, ev)
	a.done = true
	for id, ch := range a.subs {
		select {
		case ch <- ev:
		default:
		}
		close(ch)
		delete(a.subs, id)
	}
}

// subscribe returns (channel, buffered-snapshot, alreadyDone).
// When alreadyDone is true the channel is nil; the snapshot includes the done event.
func (a *activeJob) subscribe(id string) (chan rawSSEEvent, []rawSSEEvent, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	snap := make([]rawSSEEvent, len(a.buffered))
	copy(snap, a.buffered)
	if a.done {
		return nil, snap, true
	}
	ch := make(chan rawSSEEvent, 512)
	a.subs[id] = ch
	return ch, snap, false
}

func (a *activeJob) unsubscribe(id string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, ok := a.subs[id]; ok {
		delete(a.subs, id)
	}
}

// Server receives signed dispatch requests from Quill and executes them through
// a pipeline runner. It intentionally has no database dependency.
type Server struct {
	logger     *slog.Logger
	runner     pipeline.Runner
	secret     string
	router     chi.Router
	activeJobs sync.Map // jobID (string) → *activeJob
}

// New constructs a dispatcher server. secret may be empty only for local dev.
func New(logger *slog.Logger, runner pipeline.Runner, secret string) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	if runner == nil {
		runner = pipeline.NewActRunner()
	}
	s := &Server{logger: logger, runner: runner, secret: secret, router: chi.NewRouter()}
	s.router.Use(middleware.RequestID)
	s.router.Use(middleware.RealIP)
	s.router.Use(middleware.Recoverer)
	s.router.Get("/healthz", s.handleHealth)
	s.router.Post("/api/v1/runs", s.handleRun)
	s.router.Get("/api/v1/runs/{jobID}/stream", s.handleStream)
	return s
}

// ServeHTTP implements http.Handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.router.ServeHTTP(w, r)
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	httpx.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleRun accepts a signed JobSpec and starts execution asynchronously.
// It returns 202 immediately with the assigned job ID so the caller can
// connect to the /stream endpoint for live log output.
func (s *Server) handleRun(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxDispatchBody))
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid_input", "could not read request body")
		return
	}
	if !s.verifySignature(r, body) {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "invalid dispatch signature")
		return
	}
	var spec pipeline.JobSpec
	if err := json.Unmarshal(body, &spec); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid_input", "request body must be valid JSON")
		return
	}

	jobID := uuid.New().String()
	aj := newActiveJob()
	s.activeJobs.Store(jobID, aj)

	// Wire the log sink to fan events out to all SSE subscribers.
	spec.LogSink = func(jobKey, stepName, line string) {
		data, _ := json.Marshal(sseLogPayload{JobKey: jobKey, StepName: stepName, Line: line})
		aj.publish(rawSSEEvent{Event: "log", Data: data})
	}

	go func() {
		// Use background context so the run continues after the POST request ends.
		result, runErr := s.runner.Run(context.Background(), spec)

		var doneData []byte
		if runErr != nil {
			s.logger.Warn("pipeline run failed", "jobID", jobID, "error", runErr)
			doneData, _ = json.Marshal(sseDonePayload{Status: pipeline.StatusFailure, Error: runErr.Error()})
		} else {
			doneData, _ = json.Marshal(sseDonePayload{Status: result.Status, Result: &result})
		}
		aj.finish(rawSSEEvent{Event: "done", Data: doneData})

		// Keep the entry for a few minutes so late subscribers can replay logs.
		time.AfterFunc(5*time.Minute, func() { s.activeJobs.Delete(jobID) })
	}()

	httpx.JSON(w, http.StatusAccepted, map[string]string{"jobID": jobID})
}

// handleStream serves an SSE stream for a dispatched job. It replays all
// buffered events so late subscribers get the full log history.
func (s *Server) handleStream(w http.ResponseWriter, r *http.Request) {
	// This response can legitimately stay open for as long as the pipeline
	// timeout allows (default 30m, see pipeline.pipelineTimeout), far past the
	// server's WriteTimeout. That timeout exists to bound slow/stuck writes on
	// ordinary endpoints; clear it here so it can't sever a healthy long-lived
	// SSE stream out from under an in-progress job.
	_ = http.NewResponseController(w).SetWriteDeadline(time.Time{})

	jobID := chi.URLParam(r, "jobID")
	val, ok := s.activeJobs.Load(jobID)
	if !ok {
		httpx.Error(w, http.StatusNotFound, "not_found", "job not found or already expired")
		return
	}
	aj := val.(*activeJob)

	subID := uuid.New().String()
	ch, buffered, alreadyDone := aj.subscribe(subID)
	if !alreadyDone {
		defer aj.unsubscribe(subID)
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	flush := func() {
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}

	writeEv := func(ev rawSSEEvent) bool {
		_, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.Event, ev.Data)
		return err == nil
	}

	// Replay buffered events (includes the done event when alreadyDone).
	for _, ev := range buffered {
		if !writeEv(ev) {
			return
		}
	}
	flush()

	if alreadyDone {
		return
	}

	// Stream live events until the job ends or the client disconnects.
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				return // channel closed unexpectedly
			}
			if !writeEv(ev) {
				return // client disconnected
			}
			flush()
			if ev.Event == "done" {
				return
			}
		case <-ticker.C:
			if _, err := fmt.Fprintf(w, ": keepalive\n\n"); err != nil {
				return
			}
			flush()
		}
	}
}

func (s *Server) verifySignature(r *http.Request, body []byte) bool {
	if s.secret == "" {
		return true
	}
	sig := r.Header.Get("X-Quill-Dispatch-Signature")
	if sig == "" {
		return false
	}
	mac := hmac.New(sha256.New, []byte(s.secret))
	mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(expected), []byte(sig))
}
