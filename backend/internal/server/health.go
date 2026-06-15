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
func (s *Server) handleMeta(w http.ResponseWriter, r *http.Request) {
	httpx.JSON(w, http.StatusOK, map[string]any{
		"name":    "quill",
		"version": Version,
		"env":     s.cfg.Env,
		"forgejo": s.forgejoStatus(r),
	})
}

// forgejoStatus reports whether Forgejo is configured and, if so, reachable. The
// reachability probe is best-effort with a short timeout so /meta stays fast.
func (s *Server) forgejoStatus(r *http.Request) map[string]any {
	status := map[string]any{
		"configured": s.forgejo != nil && s.forgejo.Enabled(),
		"reachable":  false,
	}
	if s.forgejo == nil || !s.forgejo.Enabled() {
		return status
	}
	// Expose the public URL so frontends can construct git clone URLs. Fall back
	// to the internal base URL when a separate public URL isn't configured (fine
	// for local dev where both resolve to the same host).
	if pub := s.cfg.Forgejo.PublicURL; pub != "" {
		status["publicUrl"] = pub
	} else if base := s.cfg.Forgejo.BaseURL; base != "" {
		status["publicUrl"] = base
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if version, err := s.forgejo.Version(ctx); err == nil {
		status["reachable"] = true
		status["version"] = version
	}
	return status
}
