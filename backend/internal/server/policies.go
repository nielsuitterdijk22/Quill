package server

import (
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/nielsuitterdijk22/quill/internal/httpx"
	"github.com/nielsuitterdijk22/quill/internal/platform"
	"github.com/nielsuitterdijk22/quill/internal/store/db"
)

// This file holds the branch-policy endpoints added in PR 7: listing, upserting,
// and deleting the protection rules Quill stores for a repository's branches.
// Reads are open to project members; writes require a project owner (or platform admin)
// and are enforced in the platform service.

// branchPolicyResponse is the public JSON shape for a branch policy.
type branchPolicyResponse struct {
	Pattern               string    `json:"pattern"`
	RequiredApprovals     int       `json:"requiredApprovals"`
	DismissStaleApprovals bool      `json:"dismissStaleApprovals"`
	RequireUpToDate       bool      `json:"requireUpToDate"`
	BlockForcePush        bool      `json:"blockForcePush"`
	RequirePullRequest    bool      `json:"requirePullRequest"`
	UpdatedAt             time.Time `json:"updatedAt"`
}

func newBranchPolicyResponse(p db.BranchPolicy) branchPolicyResponse {
	return branchPolicyResponse{
		Pattern:               p.Pattern,
		RequiredApprovals:     int(p.RequiredApprovals),
		DismissStaleApprovals: p.DismissStaleApprovals,
		RequireUpToDate:       p.RequireUpToDate,
		BlockForcePush:        p.BlockForcePush,
		RequirePullRequest:    p.RequirePullRequest,
		UpdatedAt:             p.UpdatedAt,
	}
}

type setBranchPolicyRequest struct {
	Pattern               string `json:"pattern"`
	RequiredApprovals     int    `json:"requiredApprovals"`
	DismissStaleApprovals bool   `json:"dismissStaleApprovals"`
	RequireUpToDate       bool   `json:"requireUpToDate"`
	BlockForcePush        bool   `json:"blockForcePush"`
	RequirePullRequest    bool   `json:"requirePullRequest"`
}

// handleListBranchPolicies returns a repository's branch policies.
func (s *Server) handleListBranchPolicies(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	repo, policies, err := s.platform.ListBranchPolicies(r.Context(), actor, chi.URLParam(r, "slug"), chi.URLParam(r, "repo"))
	if err != nil {
		s.writePlatformError(w, err, "could not list branch policies")
		return
	}
	out := make([]branchPolicyResponse, 0, len(policies))
	for _, p := range policies {
		out = append(out, newBranchPolicyResponse(p))
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"repository": newRepoResponse(repo),
		"policies":   out,
	})
}

// handleSetBranchPolicy creates or updates a branch policy.
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
	policy, err := s.platform.SetBranchPolicy(r.Context(), actor, chi.URLParam(r, "slug"), chi.URLParam(r, "repo"), platform.BranchPolicyInput{
		Pattern:               req.Pattern,
		RequiredApprovals:     req.RequiredApprovals,
		DismissStaleApprovals: req.DismissStaleApprovals,
		RequireUpToDate:       req.RequireUpToDate,
		BlockForcePush:        req.BlockForcePush,
		RequirePullRequest:    req.RequirePullRequest,
	})
	if err != nil {
		s.writePlatformError(w, err, "could not save branch policy")
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"policy": newBranchPolicyResponse(policy)})
}

// handleDeleteBranchPolicy removes the branch policy identified by the ?pattern
// query parameter. The pattern travels as a query value because branch globs may
// contain slashes that don't fit cleanly in a path segment.
func (s *Server) handleDeleteBranchPolicy(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	pattern := strings.TrimSpace(r.URL.Query().Get("pattern"))
	if pattern == "" {
		httpx.Error(w, http.StatusBadRequest, "invalid_input", "a pattern query parameter is required")
		return
	}
	if err := s.platform.DeleteBranchPolicy(r.Context(), actor, chi.URLParam(r, "slug"), chi.URLParam(r, "repo"), pattern); err != nil {
		s.writePlatformError(w, err, "could not delete branch policy")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
