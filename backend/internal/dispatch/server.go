// Package dispatch exposes the standalone pipeline dispatcher HTTP service.
package dispatch

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/nielsuitterdijk22/quill/internal/httpx"
	"github.com/nielsuitterdijk22/quill/internal/pipeline"
)

const maxDispatchBody = 4 << 20 // 4 MiB

// Server receives signed dispatch requests from Quill and executes them through
// a pipeline runner. It intentionally has no database dependency.
type Server struct {
	logger *slog.Logger
	runner pipeline.Runner
	secret string
	router chi.Router
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
	return s
}

// ServeHTTP implements http.Handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.router.ServeHTTP(w, r)
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	httpx.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

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

	result, err := s.runner.Run(r.Context(), spec)
	if err != nil {
		s.logger.Warn("pipeline dispatch failed", "repo", spec.RepoFullName, "workflow", spec.WorkflowPath, "error", err)
		httpx.Error(w, http.StatusUnprocessableEntity, "dispatch_failed", err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, result)
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
