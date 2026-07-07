package server

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/nielsuitterdijk22/quill/internal/httpx"
	"github.com/nielsuitterdijk22/quill/internal/platform"
)

// This file holds the pipeline-secret endpoints. Secrets are encrypted key/value
// pairs exposed to CI workflows as ${{ secrets.NAME }}, managed at three scopes:
// project (shared by every repo), repository, and environment. They are
// write-only — a value is accepted on PUT but never returned — and every
// operation requires a project admin (enforced in the platform service).

// secretResponse is the public JSON shape for a secret: name and timestamps
// only, never the value.
type secretResponse struct {
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func newSecretResponse(s platform.SecretSummary) secretResponse {
	return secretResponse{Name: s.Name, CreatedAt: s.CreatedAt, UpdatedAt: s.UpdatedAt}
}

func secretResponses(secrets []platform.SecretSummary) []secretResponse {
	out := make([]secretResponse, 0, len(secrets))
	for _, s := range secrets {
		out = append(out, newSecretResponse(s))
	}
	return out
}

// setSecretRequest is the body for creating or replacing a secret. The name
// comes from the URL path.
type setSecretRequest struct {
	Value string `json:"value"`
}

// ---- project-scoped secrets ------------------------------------------------

func (s *Server) handleListProjectSecrets(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	secrets, err := s.platform.ListProjectSecrets(r.Context(), actor, chi.URLParam(r, "slug"))
	if err != nil {
		s.writePlatformError(w, err, "could not list secrets")
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"secrets": secretResponses(secrets)})
}

func (s *Server) handleSetProjectSecret(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	var req setSecretRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	name := chi.URLParam(r, "name")
	secret, err := s.platform.SetProjectSecret(r.Context(), actor, chi.URLParam(r, "slug"), name, req.Value)
	if err != nil {
		s.writePlatformError(w, err, "could not set secret")
		return
	}
	s.logAudit(r, "pipeline.secret_set", "pipeline_secret", secret.Name, map[string]any{
		"scope":   "project",
		"project": chi.URLParam(r, "slug"),
		"name":    secret.Name,
	})
	httpx.JSON(w, http.StatusOK, newSecretResponse(secret))
}

func (s *Server) handleDeleteProjectSecret(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	name := chi.URLParam(r, "name")
	if err := s.platform.DeleteProjectSecret(r.Context(), actor, chi.URLParam(r, "slug"), name); err != nil {
		s.writePlatformError(w, err, "could not delete secret")
		return
	}
	s.logAudit(r, "pipeline.secret_deleted", "pipeline_secret", name, map[string]any{
		"scope":   "project",
		"project": chi.URLParam(r, "slug"),
		"name":    name,
	})
	w.WriteHeader(http.StatusNoContent)
}

// ---- repository-scoped secrets ---------------------------------------------

func (s *Server) handleListRepoSecrets(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	secrets, err := s.platform.ListRepoSecrets(r.Context(), actor, chi.URLParam(r, "slug"), chi.URLParam(r, "repo"))
	if err != nil {
		s.writePlatformError(w, err, "could not list secrets")
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"secrets": secretResponses(secrets)})
}

func (s *Server) handleSetRepoSecret(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	var req setSecretRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	name := chi.URLParam(r, "name")
	secret, err := s.platform.SetRepoSecret(r.Context(), actor, chi.URLParam(r, "slug"), chi.URLParam(r, "repo"), name, req.Value)
	if err != nil {
		s.writePlatformError(w, err, "could not set secret")
		return
	}
	s.logAudit(r, "pipeline.secret_set", "pipeline_secret", secret.Name, map[string]any{
		"scope":   "repo",
		"project": chi.URLParam(r, "slug"),
		"repo":    chi.URLParam(r, "repo"),
		"name":    secret.Name,
	})
	httpx.JSON(w, http.StatusOK, newSecretResponse(secret))
}

func (s *Server) handleDeleteRepoSecret(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	name := chi.URLParam(r, "name")
	if err := s.platform.DeleteRepoSecret(r.Context(), actor, chi.URLParam(r, "slug"), chi.URLParam(r, "repo"), name); err != nil {
		s.writePlatformError(w, err, "could not delete secret")
		return
	}
	s.logAudit(r, "pipeline.secret_deleted", "pipeline_secret", name, map[string]any{
		"scope":   "repo",
		"project": chi.URLParam(r, "slug"),
		"repo":    chi.URLParam(r, "repo"),
		"name":    name,
	})
	w.WriteHeader(http.StatusNoContent)
}

// ---- environment-scoped secrets --------------------------------------------

func (s *Server) handleListEnvironmentSecrets(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	secrets, err := s.platform.ListEnvironmentSecrets(r.Context(), actor, chi.URLParam(r, "slug"), chi.URLParam(r, "env"))
	if err != nil {
		s.writePlatformError(w, err, "could not list secrets")
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"secrets": secretResponses(secrets)})
}

func (s *Server) handleSetEnvironmentSecret(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	var req setSecretRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	name := chi.URLParam(r, "name")
	secret, err := s.platform.SetEnvironmentSecret(r.Context(), actor, chi.URLParam(r, "slug"), chi.URLParam(r, "env"), name, req.Value)
	if err != nil {
		s.writePlatformError(w, err, "could not set secret")
		return
	}
	s.logAudit(r, "pipeline.secret_set", "pipeline_secret", secret.Name, map[string]any{
		"scope":       "environment",
		"project":     chi.URLParam(r, "slug"),
		"environment": chi.URLParam(r, "env"),
		"name":        secret.Name,
	})
	httpx.JSON(w, http.StatusOK, newSecretResponse(secret))
}

func (s *Server) handleDeleteEnvironmentSecret(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	name := chi.URLParam(r, "name")
	if err := s.platform.DeleteEnvironmentSecret(r.Context(), actor, chi.URLParam(r, "slug"), chi.URLParam(r, "env"), name); err != nil {
		s.writePlatformError(w, err, "could not delete secret")
		return
	}
	s.logAudit(r, "pipeline.secret_deleted", "pipeline_secret", name, map[string]any{
		"scope":       "environment",
		"project":     chi.URLParam(r, "slug"),
		"environment": chi.URLParam(r, "env"),
		"name":        name,
	})
	w.WriteHeader(http.StatusNoContent)
}
