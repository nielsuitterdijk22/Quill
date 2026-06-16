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

// projectResponse is the public JSON shape for a project.
type projectResponse struct {
	ID          string    `json:"id"`
	Slug        string    `json:"slug"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	ForgejoOrg  string    `json:"forgejoOrg,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
}

func newProjectResponse(p db.Project) projectResponse {
	resp := projectResponse{
		ID:          p.ID.String(),
		Slug:        p.Slug,
		Name:        p.Name,
		Description: p.Description,
		CreatedAt:   p.CreatedAt,
	}
	if p.ForgejoOrgName.Valid {
		resp.ForgejoOrg = p.ForgejoOrgName.String
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

type createProjectRequest struct {
	Slug        string `json:"slug"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

type createRepoRequest struct {
	Slug        string `json:"slug"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Visibility  string `json:"visibility"`
}

// handleListProjects returns all projects.
func (s *Server) handleListProjects(w http.ResponseWriter, r *http.Request) {
	projects, err := s.platform.ListProjects(r.Context(), 0, 0)
	if err != nil {
		s.logger.Error("list projects failed", "error", err)
		httpx.Error(w, http.StatusInternalServerError, "internal", "could not list projects")
		return
	}
	out := make([]projectResponse, 0, len(projects))
	for _, p := range projects {
		out = append(out, newProjectResponse(p))
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"projects": out})
}

// handleListMyProjects returns the projects the authenticated user belongs to.
func (s *Server) handleListMyProjects(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	rows, err := s.platform.ListMyProjects(r.Context(), actor)
	if err != nil {
		s.logger.Error("list my projects failed", "error", err)
		httpx.Error(w, http.StatusInternalServerError, "internal", "could not list projects")
		return
	}
	type myProject struct {
		projectResponse
		Role string `json:"role"`
	}
	out := make([]myProject, 0, len(rows))
	for _, row := range rows {
		out = append(out, myProject{
			projectResponse: newProjectResponse(db.Project{
				ID:             row.ID,
				TenantID:       row.TenantID,
				Slug:           row.Slug,
				Name:           row.Name,
				Description:    row.Description,
				ForgejoOrgName: row.ForgejoOrgName,
				CreatedAt:      row.CreatedAt,
			}),
			Role: row.MemberRole,
		})
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"projects": out})
}

// handleCreateProject provisions a project owned by the authenticated user.
func (s *Server) handleCreateProject(w http.ResponseWriter, r *http.Request) {
	id, ok := identityFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	var req createProjectRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	project, err := s.platform.CreateProject(r.Context(), id.UserID, platform.CreateProjectInput{
		Slug:        req.Slug,
		Name:        req.Name,
		Description: req.Description,
	})
	if err != nil {
		s.writePlatformError(w, err, "could not create project")
		return
	}
	httpx.JSON(w, http.StatusCreated, newProjectResponse(project))
}

// handleGetProject returns a single project by slug.
func (s *Server) handleGetProject(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	project, err := s.platform.GetProject(r.Context(), actor, chi.URLParam(r, "slug"))
	if err != nil {
		s.writePlatformError(w, err, "could not load project")
		return
	}
	httpx.JSON(w, http.StatusOK, newProjectResponse(project))
}

// handleListRepos returns repositories within a project.
func (s *Server) handleListRepos(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	project, repos, err := s.platform.ListReposByProject(r.Context(), actor, chi.URLParam(r, "slug"), 0, 0)
	if err != nil {
		s.writePlatformError(w, err, "could not list repositories")
		return
	}
	out := make([]repoResponse, 0, len(repos))
	for _, repo := range repos {
		out = append(out, newRepoResponse(repo))
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"project":      newProjectResponse(project),
		"repositories": out,
	})
}

// handleCreateRepo provisions a repository under a project.
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
		Slug:        req.Slug,
		Name:        req.Name,
		Description: req.Description,
		Visibility:  req.Visibility,
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
		httpx.Error(w, http.StatusForbidden, "forbidden", "you do not have access to this project")
	case errors.Is(err, platform.ErrUnavailable):
		httpx.Error(w, http.StatusBadGateway, "git_unavailable", "the git backend is unavailable for this repository")
	case errors.Is(err, platform.ErrEmptyRepo):
		httpx.Error(w, http.StatusConflict, "empty_repo", "repository has no commits yet")
	case errors.Is(err, platform.ErrPolicyViolation):
		httpx.Error(w, http.StatusConflict, "policy_violation", err.Error())
	default:
		s.logger.Error("platform operation failed", "error", err)
		httpx.Error(w, http.StatusInternalServerError, "internal", fallback)
	}
}
