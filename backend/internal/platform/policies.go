package platform

import (
	"context"
	"errors"
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/google/uuid"
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
// flow.
//
// Branch policies may be declared at three scopes — tenant, project, and repo.
// A repo inherits the policies of its project and tenant; the resolver folds
// them broad -> narrow (internal/policy.EffectiveBranch). A broader scope may
// mark a policy Locked so narrower scopes can only tighten it, never weaken it.
// Editing happens per scope; reads also surface the inherited policies a scope
// receives from above so the UI can explain where protection comes from.

// maxBranchPolicies caps the number of branch policies a single scope may hold.
const maxBranchPolicies = 50

// BranchPolicyInput is the desired state of a branch policy at one scope.
type BranchPolicyInput struct {
	Pattern               string
	RequiredApprovals     int
	DismissStaleApprovals bool
	RequireUpToDate       bool
	BlockForcePush        bool
	RequirePullRequest    bool
	RequireStatusChecks   bool
	// Locked marks the policy as a floor that narrower scopes may only tighten.
	// It is meaningful at tenant and project scope; repo scope ignores it (a
	// repo is the narrowest scope, so it has nothing to lock against).
	Locked bool
}

// BranchPolicyView is a stored branch policy in its API-facing form: the scope
// it lives at, the selector pattern, the decoded rule, and bookkeeping fields.
type BranchPolicyView struct {
	Scope     policy.ScopeType
	Pattern   string
	Rule      policy.BranchRule
	Locked    bool
	UpdatedAt time.Time
}

// BranchPolicySet groups a scope's own (editable) branch policies with the ones
// it inherits from broader scopes. Inherited policies are read-only here; they
// are edited at the scope that owns them.
type BranchPolicySet struct {
	Own       []BranchPolicyView
	Inherited []BranchPolicyView
}

// policyScope identifies a concrete scope a branch policy attaches to.
type policyScope struct {
	Type policy.ScopeType
	ID   uuid.UUID
}

// ---- repo scope -----------------------------------------------------------

// ListBranchPolicies returns a repository's own branch policies plus the ones it
// inherits from its project and tenant, for an authorized project member.
func (s *Service) ListBranchPolicies(ctx context.Context, actor Actor, projectSlug, repoSlug string) (db.Repository, BranchPolicySet, error) {
	repo, _, _, err := s.resolveRepo(ctx, actor, projectSlug, repoSlug, false)
	if err != nil {
		return db.Repository{}, BranchPolicySet{}, err
	}
	own, err := s.listBranchPoliciesAt(ctx, policyScope{policy.ScopeRepo, repo.ID})
	if err != nil {
		return db.Repository{}, BranchPolicySet{}, err
	}
	project, err := s.store.GetProjectByID(ctx, repo.ProjectID)
	if err != nil {
		return db.Repository{}, BranchPolicySet{}, fmt.Errorf("load project: %w", err)
	}
	inherited, err := s.inheritedBranchPolicies(ctx, project.ID, project.TenantID)
	if err != nil {
		return db.Repository{}, BranchPolicySet{}, err
	}
	return repo, BranchPolicySet{Own: own, Inherited: inherited}, nil
}

// SetBranchPolicy creates or updates a repository's policy for a branch pattern.
// Only project owners and platform admins may call it. The policy is recorded in
// Postgres and then mirrored (best-effort) into Forgejo branch protection.
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
	// A repo is the narrowest scope, so locking is meaningless here.
	in.Locked = false
	view, err := s.setBranchPolicyAt(ctx, policyScope{policy.ScopeRepo, repo.ID}, in)
	if err != nil {
		return BranchPolicyView{}, err
	}
	s.mirrorPolicyToForgejo(ctx, repo, project, view)
	return view, nil
}

// DeleteBranchPolicy removes a repository's policy for a branch pattern (project
// owners and platform admins only) and clears the mirrored Forgejo protection.
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
	if err := s.deleteBranchPolicyAt(ctx, policyScope{policy.ScopeRepo, repo.ID}, pattern); err != nil {
		return err
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

// ---- project scope --------------------------------------------------------

// ListProjectBranchPolicies returns a project's own branch policies plus the
// ones it inherits from its tenant, for an authorized project member.
func (s *Service) ListProjectBranchPolicies(ctx context.Context, actor Actor, projectSlug string) (db.Project, BranchPolicySet, error) {
	project, err := s.getProject(ctx, projectSlug)
	if err != nil {
		return db.Project{}, BranchPolicySet{}, err
	}
	if err := s.authorizeProjectMember(ctx, actor, project.ID); err != nil {
		return db.Project{}, BranchPolicySet{}, err
	}
	own, err := s.listBranchPoliciesAt(ctx, policyScope{policy.ScopeProject, project.ID})
	if err != nil {
		return db.Project{}, BranchPolicySet{}, err
	}
	inherited, err := s.listBranchPoliciesAt(ctx, policyScope{policy.ScopeTenant, project.TenantID})
	if err != nil {
		return db.Project{}, BranchPolicySet{}, err
	}
	return project, BranchPolicySet{Own: own, Inherited: inherited}, nil
}

// SetProjectBranchPolicy creates or updates a project-scoped branch policy that
// applies to every repository in the project. Project owners and platform admins
// only.
func (s *Service) SetProjectBranchPolicy(ctx context.Context, actor Actor, projectSlug string, in BranchPolicyInput) (BranchPolicyView, error) {
	project, err := s.getProject(ctx, projectSlug)
	if err != nil {
		return BranchPolicyView{}, err
	}
	if err := s.authorizeProjectAdmin(ctx, actor, project.ID); err != nil {
		return BranchPolicyView{}, err
	}
	return s.setBranchPolicyAt(ctx, policyScope{policy.ScopeProject, project.ID}, in)
}

// DeleteProjectBranchPolicy removes a project-scoped branch policy (project
// owners and platform admins only).
func (s *Service) DeleteProjectBranchPolicy(ctx context.Context, actor Actor, projectSlug, pattern string) error {
	project, err := s.getProject(ctx, projectSlug)
	if err != nil {
		return err
	}
	if err := s.authorizeProjectAdmin(ctx, actor, project.ID); err != nil {
		return err
	}
	return s.deleteBranchPolicyAt(ctx, policyScope{policy.ScopeProject, project.ID}, strings.TrimSpace(pattern))
}

// ---- tenant scope ---------------------------------------------------------

// ListTenantBranchPolicies returns a tenant's own branch policies. Tenant
// policies are governance set by platform admins and apply to every project and
// repository in the tenant.
func (s *Service) ListTenantBranchPolicies(ctx context.Context, actor Actor, tenantSlug string) (db.Tenant, BranchPolicySet, error) {
	if err := s.authorizePlatformAdmin(actor); err != nil {
		return db.Tenant{}, BranchPolicySet{}, err
	}
	tenant, err := s.getTenant(ctx, tenantSlug)
	if err != nil {
		return db.Tenant{}, BranchPolicySet{}, err
	}
	own, err := s.listBranchPoliciesAt(ctx, policyScope{policy.ScopeTenant, tenant.ID})
	if err != nil {
		return db.Tenant{}, BranchPolicySet{}, err
	}
	return tenant, BranchPolicySet{Own: own}, nil
}

// SetTenantBranchPolicy creates or updates a tenant-scoped branch policy.
// Platform admins only.
func (s *Service) SetTenantBranchPolicy(ctx context.Context, actor Actor, tenantSlug string, in BranchPolicyInput) (BranchPolicyView, error) {
	if err := s.authorizePlatformAdmin(actor); err != nil {
		return BranchPolicyView{}, err
	}
	tenant, err := s.getTenant(ctx, tenantSlug)
	if err != nil {
		return BranchPolicyView{}, err
	}
	return s.setBranchPolicyAt(ctx, policyScope{policy.ScopeTenant, tenant.ID}, in)
}

// DeleteTenantBranchPolicy removes a tenant-scoped branch policy. Platform admins
// only.
func (s *Service) DeleteTenantBranchPolicy(ctx context.Context, actor Actor, tenantSlug, pattern string) error {
	if err := s.authorizePlatformAdmin(actor); err != nil {
		return err
	}
	tenant, err := s.getTenant(ctx, tenantSlug)
	if err != nil {
		return err
	}
	return s.deleteBranchPolicyAt(ctx, policyScope{policy.ScopeTenant, tenant.ID}, strings.TrimSpace(pattern))
}

// ---- scope-agnostic core --------------------------------------------------

// listBranchPoliciesAt returns the branch policies declared directly at one
// scope, decoded into their API-facing view.
func (s *Service) listBranchPoliciesAt(ctx context.Context, scope policyScope) ([]BranchPolicyView, error) {
	rows, err := s.store.ListPoliciesByScope(ctx, db.ListPoliciesByScopeParams{
		ScopeType: string(scope.Type),
		ScopeID:   scope.ID,
		Kind:      string(policy.KindBranch),
	})
	if err != nil {
		return nil, fmt.Errorf("list branch policies: %w", err)
	}
	views := make([]BranchPolicyView, 0, len(rows))
	for _, row := range rows {
		view, err := branchPolicyView(row)
		if err != nil {
			return nil, err
		}
		views = append(views, view)
	}
	return views, nil
}

// setBranchPolicyAt validates and upserts a branch policy at one scope.
func (s *Service) setBranchPolicyAt(ctx context.Context, scope policyScope, in BranchPolicyInput) (BranchPolicyView, error) {
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
		ScopeType: string(scope.Type),
		ScopeID:   scope.ID,
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
		RequireStatusChecks:   in.RequireStatusChecks,
	}
	rules, err := rule.MarshalRules()
	if err != nil {
		return BranchPolicyView{}, fmt.Errorf("encode branch rule: %w", err)
	}
	row, err := s.store.UpsertPolicy(ctx, db.UpsertPolicyParams{
		ScopeType: string(scope.Type),
		ScopeID:   scope.ID,
		Kind:      string(policy.KindBranch),
		Selector:  pattern,
		Rules:     rules,
		Locked:    in.Locked,
		Enabled:   true,
	})
	if err != nil {
		return BranchPolicyView{}, fmt.Errorf("upsert branch policy: %w", err)
	}
	return branchPolicyView(row)
}

// deleteBranchPolicyAt removes a branch policy at one scope, returning
// ErrNotFound when no policy matched the selector.
func (s *Service) deleteBranchPolicyAt(ctx context.Context, scope policyScope, pattern string) error {
	rows, err := s.store.DeletePolicy(ctx, db.DeletePolicyParams{
		ScopeType: string(scope.Type),
		ScopeID:   scope.ID,
		Kind:      string(policy.KindBranch),
		Selector:  pattern,
	})
	if err != nil {
		return fmt.Errorf("delete branch policy: %w", err)
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

// inheritedBranchPolicies returns the policies a repo inherits from its project
// and tenant, ordered tenant first then project (broad -> narrow).
func (s *Service) inheritedBranchPolicies(ctx context.Context, projectID, tenantID uuid.UUID) ([]BranchPolicyView, error) {
	tenant, err := s.listBranchPoliciesAt(ctx, policyScope{policy.ScopeTenant, tenantID})
	if err != nil {
		return nil, err
	}
	project, err := s.listBranchPoliciesAt(ctx, policyScope{policy.ScopeProject, projectID})
	if err != nil {
		return nil, err
	}
	return append(tenant, project...), nil
}

// ---- helpers --------------------------------------------------------------

// getTenant loads a tenant by slug, translating a missing row to ErrNotFound.
func (s *Service) getTenant(ctx context.Context, slug string) (db.Tenant, error) {
	tenant, err := s.store.GetTenantBySlug(ctx, strings.TrimSpace(slug))
	if errors.Is(err, pgx.ErrNoRows) {
		return db.Tenant{}, ErrNotFound
	}
	if err != nil {
		return db.Tenant{}, fmt.Errorf("lookup tenant: %w", err)
	}
	return tenant, nil
}

// mirrorPolicyToForgejo reflects a policy into Forgejo branch protection so the
// git layer enforces it. It is best-effort: failures are logged but do not fail
// the request, because Quill enforces the merge gate itself for the PR flow.
// Only concrete branch names are mirrored (Forgejo rule globs differ from
// path.Match semantics, so glob policies are enforced by Quill at merge time).
//
// Only repo-scoped policies are mirrored today. Project- and tenant-scoped
// policies fan out to many repositories; mirroring their effective result per
// repo is future work — the authoritative merge gate already honours them.
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
		Scope:     policy.ScopeType(row.ScopeType),
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

// scopedPolicyInputs projects stored policy rows into the raw form the Evaluator
// consumes: the scope, selector, and the untouched rules JSON. The evaluator
// decodes the rules itself, so this carries row.Rules verbatim.
func scopedPolicyInputs(rows []db.Policy) []policy.ScopedPolicy {
	out := make([]policy.ScopedPolicy, 0, len(rows))
	for _, row := range rows {
		out = append(out, policy.ScopedPolicy{
			Scope:    policy.ScopeType(row.ScopeType),
			Selector: row.Selector,
			Rules:    row.Rules,
		})
	}
	return out
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
