package server

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/nielsuitterdijk22/quill/internal/httpx"
	"github.com/nielsuitterdijk22/quill/internal/platform"
)

// This file holds the environment-policy endpoints: listing, upserting, and
// deleting the deploy-gate rules Quill stores for an environment (name or glob).
// Like branch policies they live at three scopes — repo, project, and tenant —
// and a repo inherits its project's and tenant's rules (composed by the policy
// engine). Reads are open to project members (tenant reads require a platform
// admin); writes require a project owner at repo/project scope and a platform
// admin at tenant scope, enforced in the platform service. The selector travels
// as ?pattern on delete, matching the branch-policy convention.

// environmentPolicyResponse is the public JSON shape for an environment policy.
// scope tells the client which level declared it; locked marks a floor that
// narrower scopes may only tighten.
type environmentPolicyResponse struct {
	Scope                      string    `json:"scope"`
	Pattern                    string    `json:"pattern"`
	RequiredApprovals          int       `json:"requiredApprovals"`
	AllowedSourceBranches      []string  `json:"allowedSourceBranches"`
	RequirePreviousEnvironment string    `json:"requirePreviousEnvironment"`
	RequireSuccessfulRun       bool      `json:"requireSuccessfulRun"`
	MinWaitMinutes             int       `json:"minWaitMinutes"`
	Locked                     bool      `json:"locked"`
	UpdatedAt                  time.Time `json:"updatedAt"`
}

func newEnvironmentPolicyResponse(p platform.EnvironmentPolicyView) environmentPolicyResponse {
	sources := p.Rule.AllowedSourceBranches
	if sources == nil {
		sources = []string{}
	}
	return environmentPolicyResponse{
		Scope:                      string(p.Scope),
		Pattern:                    p.Selector,
		RequiredApprovals:          p.Rule.RequiredApprovals,
		AllowedSourceBranches:      sources,
		RequirePreviousEnvironment: p.Rule.RequirePreviousEnvironment,
		RequireSuccessfulRun:       p.Rule.RequireSuccessfulRun,
		MinWaitMinutes:             p.Rule.MinWaitMinutes,
		Locked:                     p.Locked,
		UpdatedAt:                  p.UpdatedAt,
	}
}

// environmentPolicyResponses maps a slice of views to their response shape.
func environmentPolicyResponses(views []platform.EnvironmentPolicyView) []environmentPolicyResponse {
	out := make([]environmentPolicyResponse, 0, len(views))
	for _, v := range views {
		out = append(out, newEnvironmentPolicyResponse(v))
	}
	return out
}

type setEnvironmentPolicyRequest struct {
	Pattern                    string   `json:"pattern"`
	RequiredApprovals          int      `json:"requiredApprovals"`
	AllowedSourceBranches      []string `json:"allowedSourceBranches"`
	RequirePreviousEnvironment string   `json:"requirePreviousEnvironment"`
	RequireSuccessfulRun       bool     `json:"requireSuccessfulRun"`
	MinWaitMinutes             int      `json:"minWaitMinutes"`
	Locked                     bool     `json:"locked"`
}

func (req setEnvironmentPolicyRequest) toInput() platform.EnvironmentPolicyInput {
	return platform.EnvironmentPolicyInput{
		Selector:                   req.Pattern,
		RequiredApprovals:          req.RequiredApprovals,
		AllowedSourceBranches:      req.AllowedSourceBranches,
		RequirePreviousEnvironment: req.RequirePreviousEnvironment,
		RequireSuccessfulRun:       req.RequireSuccessfulRun,
		MinWaitMinutes:             req.MinWaitMinutes,
		Locked:                     req.Locked,
	}
}

// ---- repo scope -----------------------------------------------------------

// handleListEnvironmentPolicies returns a repository's own environment policies
// plus the ones it inherits from its project and tenant.
func (s *Server) handleListEnvironmentPolicies(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	repo, set, err := s.platform.ListEnvironmentPolicies(r.Context(), actor, chi.URLParam(r, "slug"), chi.URLParam(r, "repo"))
	if err != nil {
		s.writePlatformError(w, err, "could not list environment policies")
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"repository": newRepoResponse(repo),
		"policies":   environmentPolicyResponses(set.Own),
		"inherited":  environmentPolicyResponses(set.Inherited),
	})
}

// handleSetEnvironmentPolicy creates or updates a repository environment policy.
func (s *Server) handleSetEnvironmentPolicy(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	var req setEnvironmentPolicyRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	policy, err := s.platform.SetEnvironmentPolicy(r.Context(), actor, chi.URLParam(r, "slug"), chi.URLParam(r, "repo"), req.toInput())
	if err != nil {
		s.writePlatformError(w, err, "could not save environment policy")
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"policy": newEnvironmentPolicyResponse(policy)})
}

// handleDeleteEnvironmentPolicy removes the repository policy identified by
// ?pattern.
func (s *Server) handleDeleteEnvironmentPolicy(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	pattern, ok := policyPattern(w, r)
	if !ok {
		return
	}
	if err := s.platform.DeleteEnvironmentPolicy(r.Context(), actor, chi.URLParam(r, "slug"), chi.URLParam(r, "repo"), pattern); err != nil {
		s.writePlatformError(w, err, "could not delete environment policy")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---- project scope --------------------------------------------------------

// handleListProjectEnvironmentPolicies returns a project's own environment
// policies plus the ones it inherits from its tenant.
func (s *Server) handleListProjectEnvironmentPolicies(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	project, set, err := s.platform.ListProjectEnvironmentPolicies(r.Context(), actor, chi.URLParam(r, "slug"))
	if err != nil {
		s.writePlatformError(w, err, "could not list project environment policies")
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"project":   newProjectResponse(project),
		"policies":  environmentPolicyResponses(set.Own),
		"inherited": environmentPolicyResponses(set.Inherited),
	})
}

// handleSetProjectEnvironmentPolicy creates or updates a project-scoped
// environment policy.
func (s *Server) handleSetProjectEnvironmentPolicy(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	var req setEnvironmentPolicyRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	policy, err := s.platform.SetProjectEnvironmentPolicy(r.Context(), actor, chi.URLParam(r, "slug"), req.toInput())
	if err != nil {
		s.writePlatformError(w, err, "could not save project environment policy")
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"policy": newEnvironmentPolicyResponse(policy)})
}

// handleDeleteProjectEnvironmentPolicy removes the project policy identified by
// ?pattern.
func (s *Server) handleDeleteProjectEnvironmentPolicy(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	pattern, ok := policyPattern(w, r)
	if !ok {
		return
	}
	if err := s.platform.DeleteProjectEnvironmentPolicy(r.Context(), actor, chi.URLParam(r, "slug"), pattern); err != nil {
		s.writePlatformError(w, err, "could not delete project environment policy")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---- tenant scope ---------------------------------------------------------

// handleListTenantEnvironmentPolicies returns a tenant's own environment
// policies (platform admins only).
func (s *Server) handleListTenantEnvironmentPolicies(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	tenant, set, err := s.platform.ListTenantEnvironmentPolicies(r.Context(), actor, chi.URLParam(r, "tenant"))
	if err != nil {
		s.writePlatformError(w, err, "could not list tenant environment policies")
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"tenant":   map[string]any{"slug": tenant.Slug, "name": tenant.Name},
		"policies": environmentPolicyResponses(set.Own),
	})
}

// handleSetTenantEnvironmentPolicy creates or updates a tenant-scoped
// environment policy.
func (s *Server) handleSetTenantEnvironmentPolicy(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	var req setEnvironmentPolicyRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	policy, err := s.platform.SetTenantEnvironmentPolicy(r.Context(), actor, chi.URLParam(r, "tenant"), req.toInput())
	if err != nil {
		s.writePlatformError(w, err, "could not save tenant environment policy")
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"policy": newEnvironmentPolicyResponse(policy)})
}

// handleDeleteTenantEnvironmentPolicy removes the tenant policy identified by
// ?pattern.
func (s *Server) handleDeleteTenantEnvironmentPolicy(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	pattern, ok := policyPattern(w, r)
	if !ok {
		return
	}
	if err := s.platform.DeleteTenantEnvironmentPolicy(r.Context(), actor, chi.URLParam(r, "tenant"), pattern); err != nil {
		s.writePlatformError(w, err, "could not delete tenant environment policy")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
