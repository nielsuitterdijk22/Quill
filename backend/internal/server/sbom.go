package server

import (
	"net/http"
	"regexp"

	"github.com/go-chi/chi/v5"

	"github.com/nielsuitterdijk22/quill/internal/httpx"
)

// SBOM surface (2026-11-13): status for the repo landing page, an explicit
// (re)generate, and the CycloneDX download the landing page links to.

var shaParam = regexp.MustCompile(`^[0-9a-f]{40}$`)

// handleGetSBOM: GET /projects/{slug}/repos/{repo}/sbom →
// {"sbom": head view|null, "latestReady"?: view}. Reading ensures the
// default branch head has an SBOM (generation is queued when missing).
func (s *Server) handleGetSBOM(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	status, err := s.platform.EnsureRepoSBOM(r.Context(), actor, chi.URLParam(r, "slug"), chi.URLParam(r, "repo"))
	if err != nil {
		s.writePlatformError(w, err, "could not load SBOM status")
		return
	}
	httpx.JSON(w, http.StatusOK, status)
}

// handleRequestSBOM: POST /projects/{slug}/repos/{repo}/sbom → (re)generate
// for the default branch head.
func (s *Server) handleRequestSBOM(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	view, err := s.platform.RequestRepoSBOM(r.Context(), actor, chi.URLParam(r, "slug"), chi.URLParam(r, "repo"))
	if err != nil {
		s.writePlatformError(w, err, "could not start SBOM generation")
		return
	}
	httpx.JSON(w, http.StatusAccepted, map[string]any{"sbom": view})
}

// handleDownloadSBOM: GET /projects/{slug}/repos/{repo}/sbom/download[?sha=]
// streams the CycloneDX JSON as an attachment.
func (s *Server) handleDownloadSBOM(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	sha := r.URL.Query().Get("sha")
	if sha != "" && !shaParam.MatchString(sha) {
		httpx.Error(w, http.StatusBadRequest, "invalid_input", "sha must be a full 40-character commit id")
		return
	}
	doc, name, err := s.platform.RepoSBOMDocument(r.Context(), actor, chi.URLParam(r, "slug"), chi.URLParam(r, "repo"), sha)
	if err != nil {
		s.writePlatformError(w, err, "could not load SBOM")
		return
	}
	w.Header().Set("Content-Type", "application/vnd.cyclonedx+json")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	w.Header().Set("Cache-Control", "private, max-age=300")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(doc)
}
