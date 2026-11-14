package server

import (
	"encoding/json"
	"io"
	"net/http"
	"regexp"

	"github.com/go-chi/chi/v5"

	"github.com/nielsuitterdijk22/quill/internal/httpx"
	"github.com/nielsuitterdijk22/quill/internal/platform"
)

// SBOM surface (2026-11-13): status for the repo landing page, an explicit
// (re)generate, and the CycloneDX download the landing page links to.

var shaParam = regexp.MustCompile(`^[0-9a-f]{40}$`)

// handleGetSBOM: GET /projects/{slug}/repos/{repo}/sbom →
// {"sbom": head view|null, "latestReady"?: view}; with ?sha= the status of
// that commit's SBOM ({"sbom": view|null}, nothing is queued). Reading ensures the
// default branch head has an SBOM (generation is queued when missing).
func (s *Server) handleGetSBOM(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	if sha := r.URL.Query().Get("sha"); sha != "" {
		if !shaParam.MatchString(sha) {
			httpx.Error(w, http.StatusBadRequest, "invalid_input", "sha must be a full 40-character commit id")
			return
		}
		view, err := s.platform.CommitSBOM(r.Context(), actor, chi.URLParam(r, "slug"), chi.URLParam(r, "repo"), sha)
		if err != nil {
			s.writePlatformError(w, err, "could not load SBOM status")
			return
		}
		httpx.JSON(w, http.StatusOK, map[string]any{"sbom": view})
		return
	}
	status, err := s.platform.EnsureRepoSBOM(r.Context(), actor, chi.URLParam(r, "slug"), chi.URLParam(r, "repo"))
	if err != nil {
		s.writePlatformError(w, err, "could not load SBOM status")
		return
	}
	httpx.JSON(w, http.StatusOK, status)
}

// handleRequestSBOM: POST /projects/{slug}/repos/{repo}/sbom [{"sha"}] →
// (re)generate for the default branch head, or for one older commit.
func (s *Server) handleRequestSBOM(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	var body struct {
		SHA string `json:"sha"`
	}
	if r.ContentLength > 0 {
		if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&body); err != nil {
			httpx.Error(w, http.StatusBadRequest, "invalid_input", "invalid JSON body")
			return
		}
	}
	var view *platform.SBOMView
	var err error
	if body.SHA != "" {
		if !shaParam.MatchString(body.SHA) {
			httpx.Error(w, http.StatusBadRequest, "invalid_input", "sha must be a full 40-character commit id")
			return
		}
		view, err = s.platform.RequestCommitSBOM(r.Context(), actor, chi.URLParam(r, "slug"), chi.URLParam(r, "repo"), body.SHA)
	} else {
		view, err = s.platform.RequestRepoSBOM(r.Context(), actor, chi.URLParam(r, "slug"), chi.URLParam(r, "repo"))
	}
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

// handleSBOMHistory: GET /projects/{slug}/repos/{repo}/sbom/history →
// {"sboms": [view…]} newest first.
func (s *Server) handleSBOMHistory(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	views, err := s.platform.RepoSBOMHistory(r.Context(), actor, chi.URLParam(r, "slug"), chi.URLParam(r, "repo"))
	if err != nil {
		s.writePlatformError(w, err, "could not list SBOMs")
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"sboms": views})
}

// handleSearchSBOMs: GET /sbom/search?q=&project= → components of the
// current SBOM of every repository in the caller's projects.
func (s *Server) handleSearchSBOMs(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	res, err := s.platform.SearchSBOMs(r.Context(), actor, r.URL.Query().Get("q"), r.URL.Query().Get("project"))
	if err != nil {
		s.writePlatformError(w, err, "could not search SBOMs")
		return
	}
	httpx.JSON(w, http.StatusOK, res)
}
