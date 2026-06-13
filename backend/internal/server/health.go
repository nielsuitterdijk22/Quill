package server

import (
	"context"
	"net/http"
	"time"

	"github.com/nielsuitterdijk22/quill/internal/httpx"
)

// handleHealth is a liveness probe: it returns 200 whenever the process is up.
func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	httpx.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleReady is a readiness probe. It verifies database connectivity so load
// balancers don't route traffic before the store is reachable.
func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	if s.store != nil {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := s.store.Ping(ctx); err != nil {
			httpx.Error(w, http.StatusServiceUnavailable, "not_ready", "database unreachable")
			return
		}
	}
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
