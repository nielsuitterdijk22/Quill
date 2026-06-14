package server

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/nielsuitterdijk22/quill/internal/httpx"
	"github.com/nielsuitterdijk22/quill/internal/platform"
	"github.com/nielsuitterdijk22/quill/internal/store/db"
)

// orgResponse is the public JSON shape for an organization.
type orgResponse struct {
	ID          string    `json:"id"`
	Slug        string    `json:"slug"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	ForgejoOrg  string    `json:"forgejoOrg,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
}

func newOrgResponse(o db.Organization) orgResponse {
	resp := orgResponse{
		ID:          o.ID.String(),
		Slug:        o.Slug,
		Name:        o.Name,
		Description: o.Description,
		CreatedAt:   o.CreatedAt,
	}
	if o.ForgejoOrgName.Valid {
		resp.ForgejoOrg = o.ForgejoOrgName.String
	}
	return resp
}

// repoResponse is the public JSON shape for a repository.
type repoResponse struct {
	ID            string    `json:"id"`
	Slug          string    `json:"slug"`
	Name          string    `json:"name"`
	Description   string    `json:"description"`
	Visibility    string    `json:"visibility"`
	DefaultBranch string    `json:"defaultBranch"`
	IsArchived    bool      `json:"isArchived"`
	ForgejoOwner  string    `json:"forgejoOwner,omitempty"`
	ForgejoName   string    `json:"forgejoName,omitempty"`
	CreatedAt     time.Time `json:"createdAt"`
}

func newRepoResponse(r db.Repository) repoResponse {
	resp := repoResponse{
		ID:            r.ID.String(),
		Slug:          r.Slug,
		Name:          r.Name,
		Description:   r.Description,
		Visibility:    r.Visibility,
		DefaultBranch: r.DefaultBranch,
		IsArchived:    r.IsArchived,
		CreatedAt:     r.CreatedAt,
	}
	if r.ForgejoOwner.Valid {
		resp.ForgejoOwner = r.ForgejoOwner.String
	}
	if r.ForgejoName.Valid {
		resp.ForgejoName = r.ForgejoName.String
	}
	return resp
}

type createOrgRequest struct {
	Slug        string `json:"slug"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

type createRepoRequest struct {
	Slug           string `json:"slug"`
	Name           string `json:"name"`
	Description    string `json:"description"`
	Visibility     string `json:"visibility"`
	OwningTeamSlug string `json:"owningTeamSlug"`
}

// handleListOrgs returns all organizations.
func (s *Server) handleListOrgs(w http.ResponseWriter, r *http.Request) {
	orgs, err := s.platform.ListOrgs(r.Context(), 0, 0)
	if err != nil {
		s.logger.Error("list orgs failed", "error", err)
		httpx.Error(w, http.StatusInternalServerError, "internal", "could not list organizations")
		return
	}
	out := make([]orgResponse, 0, len(orgs))
	for _, o := range orgs {
		out = append(out, newOrgResponse(o))
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"organizations": out})
}

// handleCreateOrg provisions an organization owned by the authenticated user.
func (s *Server) handleCreateOrg(w http.ResponseWriter, r *http.Request) {
	id, ok := identityFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	var req createOrgRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	org, err := s.platform.CreateOrg(r.Context(), id.UserID, platform.CreateOrgInput{
		Slug:        req.Slug,
		Name:        req.Name,
		Description: req.Description,
	})
	if err != nil {
		s.writePlatformError(w, err, "could not create organization")
		return
	}
	httpx.JSON(w, http.StatusCreated, newOrgResponse(org))
}

// handleGetOrg returns a single organization by slug.
func (s *Server) handleGetOrg(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	org, err := s.platform.GetOrg(r.Context(), actor, chi.URLParam(r, "slug"))
	if err != nil {
		s.writePlatformError(w, err, "could not load organization")
		return
	}
	httpx.JSON(w, http.StatusOK, newOrgResponse(org))
}

// handleListRepos returns repositories within an organization.
func (s *Server) handleListRepos(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	org, repos, err := s.platform.ListReposByOrg(r.Context(), actor, chi.URLParam(r, "slug"), 0, 0)
	if err != nil {
		s.writePlatformError(w, err, "could not list repositories")
		return
	}
	out := make([]repoResponse, 0, len(repos))
	for _, repo := range repos {
		out = append(out, newRepoResponse(repo))
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"organization": newOrgResponse(org),
		"repositories": out,
	})
}

// handleCreateRepo provisions a repository under an organization.
func (s *Server) handleCreateRepo(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	var req createRepoRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	repo, err := s.platform.CreateRepo(r.Context(), actor, chi.URLParam(r, "slug"), platform.CreateRepoInput{
		Slug:           req.Slug,
		Name:           req.Name,
		Description:    req.Description,
		Visibility:     req.Visibility,
		OwningTeamSlug: req.OwningTeamSlug,
	})
	if err != nil {
		s.writePlatformError(w, err, "could not create repository")
		return
	}
	httpx.JSON(w, http.StatusCreated, newRepoResponse(repo))
}

// actorFrom builds a platform.Actor from the request's authenticated identity.
func actorFrom(ctx context.Context) (platform.Actor, bool) {
	id, ok := identityFrom(ctx)
	if !ok {
		return platform.Actor{}, false
	}
	return platform.Actor{UserID: id.UserID, IsAdmin: id.IsAdmin}, true
}

// writePlatformError maps platform sentinel errors to HTTP responses.
func (s *Server) writePlatformError(w http.ResponseWriter, err error, fallback string) {
	switch {
	case errors.Is(err, platform.ErrInvalidInput):
		httpx.Error(w, http.StatusBadRequest, "invalid_input", err.Error())
	case errors.Is(err, platform.ErrConflict):
		httpx.Error(w, http.StatusConflict, "conflict", "a resource with that slug already exists")
	case errors.Is(err, platform.ErrNotFound):
		httpx.Error(w, http.StatusNotFound, "not_found", "resource not found")
	case errors.Is(err, platform.ErrForbidden):
		httpx.Error(w, http.StatusForbidden, "forbidden", "you do not have access to this organization")
	case errors.Is(err, platform.ErrUnavailable):
		httpx.Error(w, http.StatusBadGateway, "git_unavailable", "the git backend is unavailable for this repository")
	case errors.Is(err, platform.ErrPolicyViolation):
		httpx.Error(w, http.StatusConflict, "policy_violation", err.Error())
	default:
		s.logger.Error("platform operation failed", "error", err)
		httpx.Error(w, http.StatusInternalServerError, "internal", fallback)
	}
}
