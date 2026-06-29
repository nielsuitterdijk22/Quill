package server

import (
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/nielsuitterdijk22/quill/internal/httpx"
	"github.com/nielsuitterdijk22/quill/internal/platform"
)

// This file holds the branch-policy endpoints: listing, upserting, and deleting
// the protection rules Quill stores for a branch. Policies live at three scopes —
// repo, project, and tenant — and a repo inherits its project's and tenant's
// rules (folded by the policy engine). Reads are open to project members at
// repo/project scope and to any tenant member at tenant scope; writes require a
// project owner at repo/project scope and a platform admin at tenant scope, all
// enforced in the platform service.

// branchPolicyResponse is the public JSON shape for a branch policy. scope tells
// the client which level declared it; locked marks a floor that narrower scopes
// may only tighten.
type branchPolicyResponse struct {
	Scope                 string    `json:"scope"`
	Pattern               string    `json:"pattern"`
	RequiredApprovals     int       `json:"requiredApprovals"`
	DismissStaleApprovals bool      `json:"dismissStaleApprovals"`
	RequireUpToDate       bool      `json:"requireUpToDate"`
	BlockForcePush        bool      `json:"blockForcePush"`
	RequirePullRequest    bool      `json:"requirePullRequest"`
	RequireStatusChecks   bool      `json:"requireStatusChecks"`
	Locked                bool      `json:"locked"`
	UpdatedAt             time.Time `json:"updatedAt"`
}

func newBranchPolicyResponse(p platform.BranchPolicyView) branchPolicyResponse {
	return branchPolicyResponse{
		Scope:                 string(p.Scope),
		Pattern:               p.Pattern,
		RequiredApprovals:     p.Rule.RequiredApprovals,
		DismissStaleApprovals: p.Rule.DismissStaleApprovals,
		RequireUpToDate:       p.Rule.RequireUpToDate,
		BlockForcePush:        p.Rule.BlockForcePush,
		RequirePullRequest:    p.Rule.RequirePullRequest,
		RequireStatusChecks:   p.Rule.RequireStatusChecks,
		Locked:                p.Locked,
		UpdatedAt:             p.UpdatedAt,
	}
}

// branchPolicyResponses maps a slice of views to their response shape.
func branchPolicyResponses(views []platform.BranchPolicyView) []branchPolicyResponse {
	out := make([]branchPolicyResponse, 0, len(views))
	for _, v := range views {
		out = append(out, newBranchPolicyResponse(v))
	}
	return out
}

type setBranchPolicyRequest struct {
	Pattern               string `json:"pattern"`
	RequiredApprovals     int    `json:"requiredApprovals"`
	DismissStaleApprovals bool   `json:"dismissStaleApprovals"`
	RequireUpToDate       bool   `json:"requireUpToDate"`
	BlockForcePush        bool   `json:"blockForcePush"`
	RequirePullRequest    bool   `json:"requirePullRequest"`
	RequireStatusChecks   bool   `json:"requireStatusChecks"`
	Locked                bool   `json:"locked"`
}

func (req setBranchPolicyRequest) toInput() platform.BranchPolicyInput {
	return platform.BranchPolicyInput{
		Pattern:               req.Pattern,
		RequiredApprovals:     req.RequiredApprovals,
		DismissStaleApprovals: req.DismissStaleApprovals,
		RequireUpToDate:       req.RequireUpToDate,
		BlockForcePush:        req.BlockForcePush,
		RequirePullRequest:    req.RequirePullRequest,
		RequireStatusChecks:   req.RequireStatusChecks,
		Locked:                req.Locked,
	}
}

// ---- repo scope -----------------------------------------------------------

// handleListBranchPolicies returns a repository's own branch policies plus the
// ones it inherits from its project and tenant.
func (s *Server) handleListBranchPolicies(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	repo, set, err := s.platform.ListBranchPolicies(r.Context(), actor, chi.URLParam(r, "slug"), chi.URLParam(r, "repo"))
	if err != nil {
		s.writePlatformError(w, err, "could not list branch policies")
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"repository": newRepoResponse(repo),
		"policies":   branchPolicyResponses(set.Own),
		"inherited":  branchPolicyResponses(set.Inherited),
	})
}

// handleSetBranchPolicy creates or updates a repository branch policy.
func (s *Server) handleSetBranchPolicy(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	var req setBranchPolicyRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	policy, err := s.platform.SetBranchPolicy(r.Context(), actor, chi.URLParam(r, "slug"), chi.URLParam(r, "repo"), req.toInput())
	if err != nil {
		s.writePlatformError(w, err, "could not save branch policy")
		return
	}
	s.logAudit(r, "policy.updated", "branch_policy", req.Pattern, map[string]any{
		"scope":   "repo",
		"project": chi.URLParam(r, "slug"),
		"repo":    chi.URLParam(r, "repo"),
		"pattern": req.Pattern,
	})
	httpx.JSON(w, http.StatusOK, map[string]any{"policy": newBranchPolicyResponse(policy)})
}

// handleDeleteBranchPolicy removes the repository policy identified by ?pattern.
func (s *Server) handleDeleteBranchPolicy(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	pattern, ok := policyPattern(w, r)
	if !ok {
		return
	}
	if err := s.platform.DeleteBranchPolicy(r.Context(), actor, chi.URLParam(r, "slug"), chi.URLParam(r, "repo"), pattern); err != nil {
		s.writePlatformError(w, err, "could not delete branch policy")
		return
	}
	s.logAudit(r, "policy.deleted", "branch_policy", pattern, map[string]any{
		"scope":   "repo",
		"project": chi.URLParam(r, "slug"),
		"repo":    chi.URLParam(r, "repo"),
		"pattern": pattern,
	})
	w.WriteHeader(http.StatusNoContent)
}

// ---- project scope --------------------------------------------------------

// handleListProjectPolicies returns a project's own branch policies plus the
// ones it inherits from its tenant.
func (s *Server) handleListProjectPolicies(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	project, set, err := s.platform.ListProjectBranchPolicies(r.Context(), actor, chi.URLParam(r, "slug"))
	if err != nil {
		s.writePlatformError(w, err, "could not list project policies")
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"project":   newProjectResponse(project),
		"policies":  branchPolicyResponses(set.Own),
		"inherited": branchPolicyResponses(set.Inherited),
	})
}

// handleSetProjectPolicy creates or updates a project-scoped branch policy.
func (s *Server) handleSetProjectPolicy(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	var req setBranchPolicyRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	policy, err := s.platform.SetProjectBranchPolicy(r.Context(), actor, chi.URLParam(r, "slug"), req.toInput())
	if err != nil {
		s.writePlatformError(w, err, "could not save project policy")
		return
	}
	s.logAudit(r, "policy.updated", "branch_policy", req.Pattern, map[string]any{
		"scope":   "project",
		"project": chi.URLParam(r, "slug"),
		"pattern": req.Pattern,
	})
	httpx.JSON(w, http.StatusOK, map[string]any{"policy": newBranchPolicyResponse(policy)})
}

// handleDeleteProjectPolicy removes the project policy identified by ?pattern.
func (s *Server) handleDeleteProjectPolicy(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	pattern, ok := policyPattern(w, r)
	if !ok {
		return
	}
	if err := s.platform.DeleteProjectBranchPolicy(r.Context(), actor, chi.URLParam(r, "slug"), pattern); err != nil {
		s.writePlatformError(w, err, "could not delete project policy")
		return
	}
	s.logAudit(r, "policy.deleted", "branch_policy", pattern, map[string]any{
		"scope":   "project",
		"project": chi.URLParam(r, "slug"),
		"pattern": pattern,
	})
	w.WriteHeader(http.StatusNoContent)
}

// ---- tenant scope ---------------------------------------------------------

// handleListTenantPolicies returns a tenant's own branch policies (platform
// admins only).
func (s *Server) handleListTenantPolicies(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	tenant, set, err := s.platform.ListTenantBranchPolicies(r.Context(), actor, chi.URLParam(r, "tenant"))
	if err != nil {
		s.writePlatformError(w, err, "could not list tenant policies")
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"tenant":   map[string]any{"slug": tenant.Slug, "name": tenant.Name},
		"policies": branchPolicyResponses(set.Own),
	})
}

// handleSetTenantPolicy creates or updates a tenant-scoped branch policy.
func (s *Server) handleSetTenantPolicy(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	var req setBranchPolicyRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	policy, err := s.platform.SetTenantBranchPolicy(r.Context(), actor, chi.URLParam(r, "tenant"), req.toInput())
	if err != nil {
		s.writePlatformError(w, err, "could not save tenant policy")
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"policy": newBranchPolicyResponse(policy)})
}

// handleDeleteTenantPolicy removes the tenant policy identified by ?pattern.
func (s *Server) handleDeleteTenantPolicy(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	pattern, ok := policyPattern(w, r)
	if !ok {
		return
	}
	if err := s.platform.DeleteTenantBranchPolicy(r.Context(), actor, chi.URLParam(r, "tenant"), pattern); err != nil {
		s.writePlatformError(w, err, "could not delete tenant policy")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// policyPattern reads the required ?pattern query parameter. The pattern travels
// as a query value because branch globs may contain slashes that don't fit
// cleanly in a path segment. It writes a 400 and returns ok=false when missing.
func policyPattern(w http.ResponseWriter, r *http.Request) (string, bool) {
	pattern := strings.TrimSpace(r.URL.Query().Get("pattern"))
	if pattern == "" {
		httpx.Error(w, http.StatusBadRequest, "invalid_input", "a pattern query parameter is required")
		return "", false
	}
	return pattern, true
}
