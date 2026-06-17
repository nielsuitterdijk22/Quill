package platform

import (
	"context"
	"errors"
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/nielsuitterdijk22/quill/internal/forgejo"
	"github.com/nielsuitterdijk22/quill/internal/policy"
	"github.com/nielsuitterdijk22/quill/internal/store/db"
)

// This file implements branch policies on top of the unified policy engine
// (internal/policy). Branch policies are Quill-owned protection rules stored in
// Postgres as policy rows (kind=branch) and mirrored into Forgejo's branch
// protection so the git layer blocks direct pushes. Quill additionally enforces
// the review gate itself when a pull request is merged through its API (see
// enforceMergeGate in pulls.go), which is the authoritative check for the PR
// flow. Today branch policies are written at repo scope; the resolver already
// understands tenant and project scope for the inheritance work that follows.

// maxBranchPolicies caps the number of branch policies per repository.
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

// BranchPolicyView is a stored branch policy in its API-facing form: the
// selector pattern, the decoded rule, and bookkeeping fields.
type BranchPolicyView struct {
	Pattern   string
	Rule      policy.BranchRule
	Locked    bool
	UpdatedAt time.Time
}

// ListBranchPolicies returns a repository's own branch policies for an authorized
// project member.
func (s *Service) ListBranchPolicies(ctx context.Context, actor Actor, projectSlug, repoSlug string) (db.Repository, []BranchPolicyView, error) {
	repo, _, _, err := s.resolveRepo(ctx, actor, projectSlug, repoSlug, false)
	if err != nil {
		return db.Repository{}, nil, err
	}
	rows, err := s.store.ListPoliciesByScope(ctx, db.ListPoliciesByScopeParams{
		ScopeType: string(policy.ScopeRepo),
		ScopeID:   repo.ID,
		Kind:      string(policy.KindBranch),
	})
	if err != nil {
		return db.Repository{}, nil, fmt.Errorf("list branch policies: %w", err)
	}
	views := make([]BranchPolicyView, 0, len(rows))
	for _, row := range rows {
		view, err := branchPolicyView(row)
		if err != nil {
			return db.Repository{}, nil, err
		}
		views = append(views, view)
	}
	return repo, views, nil
}

// SetBranchPolicy creates or updates the policy for a branch pattern. Only project
// owners and platform admins may call it. The policy is recorded in Postgres and
// then mirrored (best-effort) into Forgejo branch protection.
func (s *Service) SetBranchPolicy(ctx context.Context, actor Actor, projectSlug, repoSlug string, in BranchPolicyInput) (BranchPolicyView, error) {
	project, err := s.getProject(ctx, projectSlug)
	if err != nil {
		return BranchPolicyView{}, err
	}
	if err := s.authorizeProjectAdmin(ctx, actor, project.ID); err != nil {
		return BranchPolicyView{}, err
	}
	repo, err := s.store.GetRepositoryBySlug(ctx, db.GetRepositoryBySlugParams{
		ProjectID: project.ID,
		Lower:     normalizeSlug(repoSlug),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return BranchPolicyView{}, ErrNotFound
	}
	if err != nil {
		return BranchPolicyView{}, fmt.Errorf("lookup repo: %w", err)
	}

	pattern := strings.TrimSpace(in.Pattern)
	if pattern == "" {
		return BranchPolicyView{}, fmt.Errorf("%w: a branch name or pattern is required", ErrInvalidInput)
	}
	if len(pattern) > 200 {
		return BranchPolicyView{}, fmt.Errorf("%w: pattern is too long", ErrInvalidInput)
	}
	if _, err := path.Match(pattern, "x"); err != nil {
		return BranchPolicyView{}, fmt.Errorf("%w: pattern is not a valid glob", ErrInvalidInput)
	}
	if in.RequiredApprovals < 0 || in.RequiredApprovals > 100 {
		return BranchPolicyView{}, fmt.Errorf("%w: required approvals must be between 0 and 100", ErrInvalidInput)
	}

	// Cap the number of policies (only when adding a new one).
	existing, err := s.store.ListPoliciesByScope(ctx, db.ListPoliciesByScopeParams{
		ScopeType: string(policy.ScopeRepo),
		ScopeID:   repo.ID,
		Kind:      string(policy.KindBranch),
	})
	if err != nil {
		return BranchPolicyView{}, fmt.Errorf("list branch policies: %w", err)
	}
	if len(existing) >= maxBranchPolicies && !hasSelector(existing, pattern) {
		return BranchPolicyView{}, fmt.Errorf("%w: too many branch policies", ErrInvalidInput)
	}

	rule := policy.BranchRule{
		RequiredApprovals:     in.RequiredApprovals,
		DismissStaleApprovals: in.DismissStaleApprovals,
		RequireUpToDate:       in.RequireUpToDate,
		BlockForcePush:        in.BlockForcePush,
		RequirePullRequest:    in.RequirePullRequest,
	}
	rules, err := rule.MarshalRules()
	if err != nil {
		return BranchPolicyView{}, fmt.Errorf("encode branch rule: %w", err)
	}
	row, err := s.store.UpsertPolicy(ctx, db.UpsertPolicyParams{
		ScopeType: string(policy.ScopeRepo),
		ScopeID:   repo.ID,
		Kind:      string(policy.KindBranch),
		Selector:  pattern,
		Rules:     rules,
		Locked:    false,
		Enabled:   true,
	})
	if err != nil {
		return BranchPolicyView{}, fmt.Errorf("upsert branch policy: %w", err)
	}

	view, err := branchPolicyView(row)
	if err != nil {
		return BranchPolicyView{}, err
	}
	s.mirrorPolicyToForgejo(ctx, repo, project, view)
	return view, nil
}

// DeleteBranchPolicy removes the policy for a branch pattern (project owners and
// platform admins only) and clears the mirrored Forgejo branch protection.
func (s *Service) DeleteBranchPolicy(ctx context.Context, actor Actor, projectSlug, repoSlug, pattern string) error {
	project, err := s.getProject(ctx, projectSlug)
	if err != nil {
		return err
	}
	if err := s.authorizeProjectAdmin(ctx, actor, project.ID); err != nil {
		return err
	}
	repo, err := s.store.GetRepositoryBySlug(ctx, db.GetRepositoryBySlugParams{
		ProjectID: project.ID,
		Lower:     normalizeSlug(repoSlug),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("lookup repo: %w", err)
	}
	pattern = strings.TrimSpace(pattern)
	rows, err := s.store.DeletePolicy(ctx, db.DeletePolicyParams{
		ScopeType: string(policy.ScopeRepo),
		ScopeID:   repo.ID,
		Kind:      string(policy.KindBranch),
		Selector:  pattern,
	})
	if err != nil {
		return fmt.Errorf("delete branch policy: %w", err)
	}
	if rows == 0 {
		return ErrNotFound
	}
	// Best-effort: clear the mirrored protection for an exact branch name.
	if s.forgejoEnabled() && !strings.ContainsAny(pattern, "*?[") {
		if owner, name, ok := forgejoTarget(repo, project); ok {
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
func (s *Service) mirrorPolicyToForgejo(ctx context.Context, repo db.Repository, project db.Project, view BranchPolicyView) {
	if !s.forgejoEnabled() || strings.ContainsAny(view.Pattern, "*?[") {
		return
	}
	owner, name, ok := forgejoTarget(repo, project)
	if !ok {
		return
	}
	err := s.forgejo.UpsertBranchProtection(ctx, owner, name, forgejo.BranchProtectionOptions{
		BranchName:             view.Pattern,
		EnablePush:             !view.Rule.RequirePullRequest,
		RequiredApprovals:      int64(view.Rule.RequiredApprovals),
		DismissStaleApprovals:  view.Rule.DismissStaleApprovals,
		BlockOnRejectedReviews: true,
		BlockOnOutdatedBranch:  view.Rule.RequireUpToDate,
	})
	if err != nil {
		s.logger.Warn("could not mirror branch policy to forgejo",
			"repo", owner+"/"+name, "branch", view.Pattern, "error", err)
	}
}

// branchPolicyView decodes a stored policy row into its API-facing view.
func branchPolicyView(row db.Policy) (BranchPolicyView, error) {
	rule, err := policy.DecodeBranchRule(row.Rules)
	if err != nil {
		return BranchPolicyView{}, fmt.Errorf("decode branch rule: %w", err)
	}
	return BranchPolicyView{
		Pattern:   row.Selector,
		Rule:      rule,
		Locked:    row.Locked,
		UpdatedAt: row.UpdatedAt,
	}, nil
}

// scopedBranchPolicies decodes stored policy rows into resolver inputs.
func scopedBranchPolicies(rows []db.Policy) ([]policy.ScopedBranch, error) {
	out := make([]policy.ScopedBranch, 0, len(rows))
	for _, row := range rows {
		rule, err := policy.DecodeBranchRule(row.Rules)
		if err != nil {
			return nil, fmt.Errorf("decode branch rule: %w", err)
		}
		out = append(out, policy.ScopedBranch{
			Scope:    policy.ScopeType(row.ScopeType),
			Selector: row.Selector,
			Locked:   row.Locked,
			Rule:     rule,
		})
	}
	return out, nil
}

// hasSelector reports whether rows already contains a policy with selector.
func hasSelector(rows []db.Policy, selector string) bool {
	for i := range rows {
		if rows[i].Selector == selector {
			return true
		}
	}
	return false
}
