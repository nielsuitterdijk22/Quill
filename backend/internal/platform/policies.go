package platform

import (
	"context"
	"errors"
	"fmt"
	"path"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/nielsuitterdijk22/quill/internal/forgejo"
	"github.com/nielsuitterdijk22/quill/internal/store/db"
)

// This file implements branch policies: Quill-owned protection rules stored in
// Postgres (the source of truth) and mirrored into Forgejo's branch protection
// so the git layer blocks direct pushes. Quill additionally enforces the review
// gate itself when a pull request is merged through its API (see enforceMergeGate
// in pulls.go), which is the authoritative check for the PR flow.

// maxBranchPolicies caps the number of policies per repository.
const maxBranchPolicies = 50

// BranchPolicyInput is the desired state of a branch policy.
type BranchPolicyInput struct {
	Pattern               string
	RequiredApprovals     int
	DismissStaleApprovals bool
	RequireUpToDate       bool
	BlockForcePush        bool
	RequirePullRequest    bool
}

// ListBranchPolicies returns a repository's branch policies for an authorized
// org member.
func (s *Service) ListBranchPolicies(ctx context.Context, actor Actor, orgSlug, repoSlug string) (db.Repository, []db.BranchPolicy, error) {
	repo, _, _, err := s.resolveRepo(ctx, actor, orgSlug, repoSlug, false)
	if err != nil {
		return db.Repository{}, nil, err
	}
	policies, err := s.store.ListBranchPoliciesByRepo(ctx, repo.ID)
	if err != nil {
		return db.Repository{}, nil, fmt.Errorf("list branch policies: %w", err)
	}
	return repo, policies, nil
}

// SetBranchPolicy creates or updates the policy for a branch pattern. Only org
// owners and platform admins may call it. The policy is recorded in Postgres and
// then mirrored (best-effort) into Forgejo branch protection.
func (s *Service) SetBranchPolicy(ctx context.Context, actor Actor, orgSlug, repoSlug string, in BranchPolicyInput) (db.BranchPolicy, error) {
	org, err := s.getOrg(ctx, orgSlug)
	if err != nil {
		return db.BranchPolicy{}, err
	}
	if err := s.authorizeOrgAdmin(ctx, actor, org.ID); err != nil {
		return db.BranchPolicy{}, err
	}
	repo, err := s.store.GetRepositoryBySlug(ctx, db.GetRepositoryBySlugParams{
		OrgID: org.ID,
		Lower: normalizeSlug(repoSlug),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return db.BranchPolicy{}, ErrNotFound
	}
	if err != nil {
		return db.BranchPolicy{}, fmt.Errorf("lookup repo: %w", err)
	}

	pattern := strings.TrimSpace(in.Pattern)
	if pattern == "" {
		return db.BranchPolicy{}, fmt.Errorf("%w: a branch name or pattern is required", ErrInvalidInput)
	}
	if len(pattern) > 200 {
		return db.BranchPolicy{}, fmt.Errorf("%w: pattern is too long", ErrInvalidInput)
	}
	if _, err := path.Match(pattern, "x"); err != nil {
		return db.BranchPolicy{}, fmt.Errorf("%w: pattern is not a valid glob", ErrInvalidInput)
	}
	if in.RequiredApprovals < 0 || in.RequiredApprovals > 100 {
		return db.BranchPolicy{}, fmt.Errorf("%w: required approvals must be between 0 and 100", ErrInvalidInput)
	}

	// Cap the number of policies (only when adding a new one).
	existing, err := s.store.ListBranchPoliciesByRepo(ctx, repo.ID)
	if err != nil {
		return db.BranchPolicy{}, fmt.Errorf("list branch policies: %w", err)
	}
	if len(existing) >= maxBranchPolicies && !hasPolicyPattern(existing, pattern) {
		return db.BranchPolicy{}, fmt.Errorf("%w: too many branch policies", ErrInvalidInput)
	}

	policy, err := s.store.UpsertBranchPolicy(ctx, db.UpsertBranchPolicyParams{
		RepoID:                repo.ID,
		Pattern:               pattern,
		RequiredApprovals:     int32(in.RequiredApprovals),
		DismissStaleApprovals: in.DismissStaleApprovals,
		RequireUpToDate:       in.RequireUpToDate,
		BlockForcePush:        in.BlockForcePush,
		RequirePullRequest:    in.RequirePullRequest,
	})
	if err != nil {
		return db.BranchPolicy{}, fmt.Errorf("upsert branch policy: %w", err)
	}

	s.mirrorPolicyToForgejo(ctx, repo, org, policy)
	return policy, nil
}

// DeleteBranchPolicy removes the policy for a branch pattern (org owners and
// platform admins only) and clears the mirrored Forgejo branch protection.
func (s *Service) DeleteBranchPolicy(ctx context.Context, actor Actor, orgSlug, repoSlug, pattern string) error {
	org, err := s.getOrg(ctx, orgSlug)
	if err != nil {
		return err
	}
	if err := s.authorizeOrgAdmin(ctx, actor, org.ID); err != nil {
		return err
	}
	repo, err := s.store.GetRepositoryBySlug(ctx, db.GetRepositoryBySlugParams{
		OrgID: org.ID,
		Lower: normalizeSlug(repoSlug),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("lookup repo: %w", err)
	}
	pattern = strings.TrimSpace(pattern)
	rows, err := s.store.DeleteBranchPolicy(ctx, db.DeleteBranchPolicyParams{RepoID: repo.ID, Pattern: pattern})
	if err != nil {
		return fmt.Errorf("delete branch policy: %w", err)
	}
	if rows == 0 {
		return ErrNotFound
	}
	// Best-effort: clear the mirrored protection for an exact branch name.
	if s.forgejoEnabled() && !strings.ContainsAny(pattern, "*?[") {
		if owner, name, ok := forgejoTarget(repo, org); ok {
			if err := s.forgejo.DeleteBranchProtection(ctx, owner, name, pattern); err != nil {
				s.logger.Warn("could not delete forgejo branch protection",
					"repo", owner+"/"+name, "branch", pattern, "error", err)
			}
		}
	}
	return nil
}

// mirrorPolicyToForgejo reflects a policy into Forgejo branch protection so the
// git layer enforces it. It is best-effort: failures are logged but do not fail
// the request, because Quill enforces the merge gate itself for the PR flow.
// Only concrete branch names are mirrored (Forgejo rule globs differ from
// path.Match semantics, so glob policies are enforced by Quill at merge time).
func (s *Service) mirrorPolicyToForgejo(ctx context.Context, repo db.Repository, org db.Organization, policy db.BranchPolicy) {
	if !s.forgejoEnabled() || strings.ContainsAny(policy.Pattern, "*?[") {
		return
	}
	owner, name, ok := forgejoTarget(repo, org)
	if !ok {
		return
	}
	err := s.forgejo.UpsertBranchProtection(ctx, owner, name, forgejo.BranchProtectionOptions{
		BranchName:             policy.Pattern,
		EnablePush:             !policy.RequirePullRequest,
		RequiredApprovals:      int64(policy.RequiredApprovals),
		DismissStaleApprovals:  policy.DismissStaleApprovals,
		BlockOnRejectedReviews: true,
		BlockOnOutdatedBranch:  policy.RequireUpToDate,
	})
	if err != nil {
		s.logger.Warn("could not mirror branch policy to forgejo",
			"repo", owner+"/"+name, "branch", policy.Pattern, "error", err)
	}
}

// matchBranchPolicy returns the policy governing a branch: an exact pattern match
// wins, otherwise the first glob pattern (ordered by pattern) that matches. nil
// when no policy applies.
func matchBranchPolicy(policies []db.BranchPolicy, branch string) *db.BranchPolicy {
	for i := range policies {
		if policies[i].Pattern == branch {
			return &policies[i]
		}
	}
	for i := range policies {
		if ok, err := path.Match(policies[i].Pattern, branch); err == nil && ok {
			return &policies[i]
		}
	}
	return nil
}

// hasPolicyPattern reports whether policies already contains pattern.
func hasPolicyPattern(policies []db.BranchPolicy, pattern string) bool {
	for i := range policies {
		if policies[i].Pattern == pattern {
			return true
		}
	}
	return false
}
