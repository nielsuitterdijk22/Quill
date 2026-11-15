package server

import (
	"net/http"
	"strconv"

	"github.com/nielsuitterdijk22/quill/internal/httpx"
)

// handleGetAnalytics returns operator analytics (admin only): signups and
// activation, plan usage per organization, feature usage. ?days= sets the
// window (default 30).
func (s *Server) handleGetAnalytics(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	days, _ := strconv.Atoi(r.URL.Query().Get("days"))
	out, err := s.platform.GetAnalytics(r.Context(), actor, days)
	if err != nil {
		s.writePlatformError(w, err, "could not load analytics")
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}
