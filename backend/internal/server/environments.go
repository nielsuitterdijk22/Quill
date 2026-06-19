package server

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/nielsuitterdijk22/quill/internal/httpx"
	"github.com/nielsuitterdijk22/quill/internal/platform"
	"github.com/nielsuitterdijk22/quill/internal/store/db"
)

// This file holds the environment endpoints: listing, creating, reading,
// updating, and deleting a project's deployment targets (staging, production,
// …). Environments are project-owned platform metadata with a promotion rank;
// reads are open to project members, writes require a project admin (enforced in
// the platform service). The deploy flow that gates deploys against these
// targets lands in a later PR.

// environmentResponse is the public JSON shape for an environment.
type environmentResponse struct {
	ID          string    `json:"id"`
	Slug        string    `json:"slug"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Rank        int       `json:"rank"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

func newEnvironmentResponse(e db.Environment) environmentResponse {
	return environmentResponse{
		ID:          e.ID.String(),
		Slug:        e.Slug,
		Name:        e.Name,
		Description: e.Description,
		Rank:        int(e.Rank),
		CreatedAt:   e.CreatedAt,
		UpdatedAt:   e.UpdatedAt,
	}
}

func environmentResponses(envs []db.Environment) []environmentResponse {
	out := make([]environmentResponse, 0, len(envs))
	for _, e := range envs {
		out = append(out, newEnvironmentResponse(e))
	}
	return out
}

type createEnvironmentRequest struct {
	Slug        string `json:"slug"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Rank        int    `json:"rank"`
}

// updateEnvironmentRequest is the full replacement of an environment's mutable
// fields. The slug is immutable, so it is not accepted here.
type updateEnvironmentRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Rank        int    `json:"rank"`
}

// handleListEnvironments returns a project's environments ordered by rank.
func (s *Server) handleListEnvironments(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	project, envs, err := s.platform.ListEnvironments(r.Context(), actor, chi.URLParam(r, "slug"))
	if err != nil {
		s.writePlatformError(w, err, "could not list environments")
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"project":      newProjectResponse(project),
		"environments": environmentResponses(envs),
	})
}

// handleCreateEnvironment defines a new environment under a project.
func (s *Server) handleCreateEnvironment(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	var req createEnvironmentRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	env, err := s.platform.CreateEnvironment(r.Context(), actor, chi.URLParam(r, "slug"), platform.CreateEnvironmentInput{
		Slug:        req.Slug,
		Name:        req.Name,
		Description: req.Description,
		Rank:        req.Rank,
	})
	if err != nil {
		s.writePlatformError(w, err, "could not create environment")
		return
	}
	httpx.JSON(w, http.StatusCreated, newEnvironmentResponse(env))
}

// handleGetEnvironment returns a single environment by slug.
func (s *Server) handleGetEnvironment(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	env, err := s.platform.GetEnvironment(r.Context(), actor, chi.URLParam(r, "slug"), chi.URLParam(r, "env"))
	if err != nil {
		s.writePlatformError(w, err, "could not load environment")
		return
	}
	httpx.JSON(w, http.StatusOK, newEnvironmentResponse(env))
}

// handleUpdateEnvironment changes an environment's display fields and rank.
func (s *Server) handleUpdateEnvironment(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	var req updateEnvironmentRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	env, err := s.platform.UpdateEnvironment(r.Context(), actor, chi.URLParam(r, "slug"), chi.URLParam(r, "env"), platform.UpdateEnvironmentInput{
		Name:        req.Name,
		Description: req.Description,
		Rank:        req.Rank,
	})
	if err != nil {
		s.writePlatformError(w, err, "could not update environment")
		return
	}
	httpx.JSON(w, http.StatusOK, newEnvironmentResponse(env))
}

// handleDeleteEnvironment removes an environment.
func (s *Server) handleDeleteEnvironment(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	if err := s.platform.DeleteEnvironment(r.Context(), actor, chi.URLParam(r, "slug"), chi.URLParam(r, "env")); err != nil {
		s.writePlatformError(w, err, "could not delete environment")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
