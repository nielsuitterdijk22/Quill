package server

import (
	"net/http"

	"github.com/nielsuitterdijk22/quill/internal/httpx"
)

// handleHealth is a liveness probe: it returns 200 whenever the process is up.
func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	httpx.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleReady is a readiness probe. From PR 2 onward it will verify dependencies
// (Postgres, Forgejo) before reporting ready.
func (s *Server) handleReady(w http.ResponseWriter, _ *http.Request) {
	httpx.JSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

// handleMeta reports basic service metadata, useful as a frontend connectivity check.
func (s *Server) handleMeta(w http.ResponseWriter, _ *http.Request) {
	httpx.JSON(w, http.StatusOK, map[string]any{
		"name":    "quill",
		"version": Version,
		"env":     s.cfg.Env,
	})
}
