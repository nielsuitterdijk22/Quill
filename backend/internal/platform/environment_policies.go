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

	"github.com/nielsuitterdijk22/quill/internal/policy"
	"github.com/nielsuitterdijk22/quill/internal/store/db"
)

// This file implements environment (deploy-gate) policies on top of the unified
// policy engine (internal/policy), mirroring branch policies. Environment
// policies are Quill-owned rules stored as policy rows (kind=environment) that
// govern whether a commit may deploy to an environment matching the policy's
// selector (an environment name or glob). They are declared at three scopes —
// tenant, project, and repo — and resolve through the same broad -> narrow,
// lockable inheritance as branch policies (decision #3 in plan.md). Editing
// happens per scope; reads also surface inherited policies so the UI can explain
// where a deploy gate comes from.
//
// Unlike branch policies there is no Forgejo mirror: environments and deploys are
// Quill-owned, so the deploy gate is enforced entirely by Quill (the enforcement
// path lands with the deploy flow; this file is the configuration surface).

// maxEnvironmentPolicies caps the number of environment policies a single scope
// may hold.
const maxEnvironmentPolicies = 50

// EnvironmentPolicyInput is the desired state of an environment policy at one
// scope.
type EnvironmentPolicyInput struct {
	// Selector is the environment name or glob the policy governs (e.g.
	// "production", "prod-*").
	Selector string
	// RequiredApprovals is the number of deploy approvals required.
	RequiredApprovals int
	// AllowedSourceBranches restricts which refs may deploy to the environment.
	// Each entry is a glob matched against the deploy ref. Empty means any ref.
	AllowedSourceBranches []string
	// RequirePreviousEnvironment names an environment that must have a successful
	// deploy of the same commit first (ordered promotion). Empty means none.
	RequirePreviousEnvironment string
	// RequireSuccessfulRun blocks the deploy unless the commit has a green run.
	RequireSuccessfulRun bool
	// MinWaitMinutes is a soak/freeze window before a candidate may deploy.
	MinWaitMinutes int
	// Locked marks the policy as a floor that narrower scopes may only tighten.
	// Meaningful at tenant and project scope; repo scope ignores it.
	Locked bool
}

// EnvironmentPolicyView is a stored environment policy in its API-facing form.
type EnvironmentPolicyView struct {
	Scope     policy.ScopeType
	Selector  string
	Rule      policy.EnvironmentRule
	Locked    bool
	UpdatedAt time.Time
}

// EnvironmentPolicySet groups a scope's own (editable) environment policies with
// the ones it inherits from broader scopes (read-only here).
type EnvironmentPolicySet struct {
	Own       []EnvironmentPolicyView
	Inherited []EnvironmentPolicyView
}

// ---- repo scope -----------------------------------------------------------

// ListEnvironmentPolicies returns a repository's own environment policies plus
// the ones it inherits from its project and tenant, for an authorized member.
func (s *Service) ListEnvironmentPolicies(ctx context.Context, actor Actor, projectSlug, repoSlug string) (db.Repository, EnvironmentPolicySet, error) {
	repo, _, _, err := s.resolveRepo(ctx, actor, projectSlug, repoSlug, false)
	if err != nil {
		return db.Repository{}, EnvironmentPolicySet{}, err
	}
	own, err := s.listEnvironmentPoliciesAt(ctx, policyScope{policy.ScopeRepo, repo.ID})
	if err != nil {
		return db.Repository{}, EnvironmentPolicySet{}, err
	}
	project, err := s.store.GetProjectByID(ctx, repo.ProjectID)
	if err != nil {
		return db.Repository{}, EnvironmentPolicySet{}, fmt.Errorf("load project: %w", err)
	}
	inherited, err := s.inheritedEnvironmentPolicies(ctx, project.ID, project.TenantID)
	if err != nil {
		return db.Repository{}, EnvironmentPolicySet{}, err
	}
	return repo, EnvironmentPolicySet{Own: own, Inherited: inherited}, nil
}

// SetEnvironmentPolicy creates or updates a repository's policy for an
// environment selector. Project owners and platform admins only.
func (s *Service) SetEnvironmentPolicy(ctx context.Context, actor Actor, projectSlug, repoSlug string, in EnvironmentPolicyInput) (EnvironmentPolicyView, error) {
	project, err := s.getProject(ctx, projectSlug)
	if err != nil {
		return EnvironmentPolicyView{}, err
	}
	if err := s.authorizeProjectAdmin(ctx, actor, project.ID); err != nil {
		return EnvironmentPolicyView{}, err
	}
	repo, err := s.store.GetRepositoryBySlug(ctx, db.GetRepositoryBySlugParams{
		ProjectID: project.ID,
		Lower:     normalizeSlug(repoSlug),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return EnvironmentPolicyView{}, ErrNotFound
	}
	if err != nil {
		return EnvironmentPolicyView{}, fmt.Errorf("lookup repo: %w", err)
	}
	// A repo is the narrowest scope, so locking is meaningless here.
	in.Locked = false
	return s.setEnvironmentPolicyAt(ctx, policyScope{policy.ScopeRepo, repo.ID}, in)
}

// DeleteEnvironmentPolicy removes a repository's policy for an environment
// selector (project owners and platform admins only).
func (s *Service) DeleteEnvironmentPolicy(ctx context.Context, actor Actor, projectSlug, repoSlug, selector string) error {
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
	return s.deleteEnvironmentPolicyAt(ctx, policyScope{policy.ScopeRepo, repo.ID}, strings.TrimSpace(selector))
}

// ---- project scope --------------------------------------------------------

// ListProjectEnvironmentPolicies returns a project's own environment policies
// plus the ones it inherits from its tenant, for an authorized project member.
func (s *Service) ListProjectEnvironmentPolicies(ctx context.Context, actor Actor, projectSlug string) (db.Project, EnvironmentPolicySet, error) {
	project, err := s.getProject(ctx, projectSlug)
	if err != nil {
		return db.Project{}, EnvironmentPolicySet{}, err
	}
	if err := s.authorizeProjectMember(ctx, actor, project.ID); err != nil {
		return db.Project{}, EnvironmentPolicySet{}, err
	}
	own, err := s.listEnvironmentPoliciesAt(ctx, policyScope{policy.ScopeProject, project.ID})
	if err != nil {
		return db.Project{}, EnvironmentPolicySet{}, err
	}
	inherited, err := s.listEnvironmentPoliciesAt(ctx, policyScope{policy.ScopeTenant, project.TenantID})
	if err != nil {
		return db.Project{}, EnvironmentPolicySet{}, err
	}
	return project, EnvironmentPolicySet{Own: own, Inherited: inherited}, nil
}

// SetProjectEnvironmentPolicy creates or updates a project-scoped environment
// policy that applies to every repository in the project. Project owners and
// platform admins only.
func (s *Service) SetProjectEnvironmentPolicy(ctx context.Context, actor Actor, projectSlug string, in EnvironmentPolicyInput) (EnvironmentPolicyView, error) {
	project, err := s.getProject(ctx, projectSlug)
	if err != nil {
		return EnvironmentPolicyView{}, err
	}
	if err := s.authorizeProjectAdmin(ctx, actor, project.ID); err != nil {
		return EnvironmentPolicyView{}, err
	}
	return s.setEnvironmentPolicyAt(ctx, policyScope{policy.ScopeProject, project.ID}, in)
}

// DeleteProjectEnvironmentPolicy removes a project-scoped environment policy
// (project owners and platform admins only).
func (s *Service) DeleteProjectEnvironmentPolicy(ctx context.Context, actor Actor, projectSlug, selector string) error {
	project, err := s.getProject(ctx, projectSlug)
	if err != nil {
		return err
	}
	if err := s.authorizeProjectAdmin(ctx, actor, project.ID); err != nil {
		return err
	}
	return s.deleteEnvironmentPolicyAt(ctx, policyScope{policy.ScopeProject, project.ID}, strings.TrimSpace(selector))
}

// ---- tenant scope ---------------------------------------------------------

// ListTenantEnvironmentPolicies returns a tenant's own environment policies
// (platform admins only).
func (s *Service) ListTenantEnvironmentPolicies(ctx context.Context, actor Actor, tenantSlug string) (db.Tenant, EnvironmentPolicySet, error) {
	if err := s.authorizePlatformAdmin(actor); err != nil {
		return db.Tenant{}, EnvironmentPolicySet{}, err
	}
	tenant, err := s.getTenant(ctx, tenantSlug)
	if err != nil {
		return db.Tenant{}, EnvironmentPolicySet{}, err
	}
	own, err := s.listEnvironmentPoliciesAt(ctx, policyScope{policy.ScopeTenant, tenant.ID})
	if err != nil {
		return db.Tenant{}, EnvironmentPolicySet{}, err
	}
	return tenant, EnvironmentPolicySet{Own: own}, nil
}

// SetTenantEnvironmentPolicy creates or updates a tenant-scoped environment
// policy. Platform admins only.
func (s *Service) SetTenantEnvironmentPolicy(ctx context.Context, actor Actor, tenantSlug string, in EnvironmentPolicyInput) (EnvironmentPolicyView, error) {
	if err := s.authorizePlatformAdmin(actor); err != nil {
		return EnvironmentPolicyView{}, err
	}
	tenant, err := s.getTenant(ctx, tenantSlug)
	if err != nil {
		return EnvironmentPolicyView{}, err
	}
	return s.setEnvironmentPolicyAt(ctx, policyScope{policy.ScopeTenant, tenant.ID}, in)
}

// DeleteTenantEnvironmentPolicy removes a tenant-scoped environment policy.
// Platform admins only.
func (s *Service) DeleteTenantEnvironmentPolicy(ctx context.Context, actor Actor, tenantSlug, selector string) error {
	if err := s.authorizePlatformAdmin(actor); err != nil {
		return err
	}
	tenant, err := s.getTenant(ctx, tenantSlug)
	if err != nil {
		return err
	}
	return s.deleteEnvironmentPolicyAt(ctx, policyScope{policy.ScopeTenant, tenant.ID}, strings.TrimSpace(selector))
}

// ---- scope-agnostic core --------------------------------------------------

// listEnvironmentPoliciesAt returns the environment policies declared directly
// at one scope, decoded into their API-facing view.
func (s *Service) listEnvironmentPoliciesAt(ctx context.Context, scope policyScope) ([]EnvironmentPolicyView, error) {
	rows, err := s.store.ListPoliciesByScope(ctx, db.ListPoliciesByScopeParams{
		ScopeType: string(scope.Type),
		ScopeID:   scope.ID,
		Kind:      string(policy.KindEnvironment),
	})
	if err != nil {
		return nil, fmt.Errorf("list environment policies: %w", err)
	}
	views := make([]EnvironmentPolicyView, 0, len(rows))
	for _, row := range rows {
		view, err := environmentPolicyView(row)
		if err != nil {
			return nil, err
		}
		views = append(views, view)
	}
	return views, nil
}

// setEnvironmentPolicyAt validates and upserts an environment policy at one
// scope.
func (s *Service) setEnvironmentPolicyAt(ctx context.Context, scope policyScope, in EnvironmentPolicyInput) (EnvironmentPolicyView, error) {
	selector := strings.TrimSpace(in.Selector)
	if selector == "" {
		return EnvironmentPolicyView{}, fmt.Errorf("%w: an environment name or pattern is required", ErrInvalidInput)
	}
	if len(selector) > 200 {
		return EnvironmentPolicyView{}, fmt.Errorf("%w: selector is too long", ErrInvalidInput)
	}
	if _, err := path.Match(selector, "x"); err != nil {
		return EnvironmentPolicyView{}, fmt.Errorf("%w: selector is not a valid glob", ErrInvalidInput)
	}
	if in.RequiredApprovals < 0 || in.RequiredApprovals > 100 {
		return EnvironmentPolicyView{}, fmt.Errorf("%w: required approvals must be between 0 and 100", ErrInvalidInput)
	}
	if in.MinWaitMinutes < 0 || in.MinWaitMinutes > 100000 {
		return EnvironmentPolicyView{}, fmt.Errorf("%w: wait minutes must be between 0 and 100000", ErrInvalidInput)
	}
	sources := normalizeSources(in.AllowedSourceBranches)
	for _, src := range sources {
		if _, err := path.Match(src, "x"); err != nil {
			return EnvironmentPolicyView{}, fmt.Errorf("%w: allowed source %q is not a valid glob", ErrInvalidInput, src)
		}
	}
	prev := strings.TrimSpace(in.RequirePreviousEnvironment)
	if len(prev) > 200 {
		return EnvironmentPolicyView{}, fmt.Errorf("%w: previous environment name is too long", ErrInvalidInput)
	}

	// Cap the number of policies (only when adding a new one).
	existing, err := s.store.ListPoliciesByScope(ctx, db.ListPoliciesByScopeParams{
		ScopeType: string(scope.Type),
		ScopeID:   scope.ID,
		Kind:      string(policy.KindEnvironment),
	})
	if err != nil {
		return EnvironmentPolicyView{}, fmt.Errorf("list environment policies: %w", err)
	}
	if len(existing) >= maxEnvironmentPolicies && !hasSelector(existing, selector) {
		return EnvironmentPolicyView{}, fmt.Errorf("%w: too many environment policies", ErrInvalidInput)
	}

	rule := policy.EnvironmentRule{
		RequiredApprovals:          in.RequiredApprovals,
		AllowedSourceBranches:      sources,
		RequirePreviousEnvironment: prev,
		RequireSuccessfulRun:       in.RequireSuccessfulRun,
		MinWaitMinutes:             in.MinWaitMinutes,
	}
	rules, err := rule.MarshalRules()
	if err != nil {
		return EnvironmentPolicyView{}, fmt.Errorf("encode environment rule: %w", err)
	}
	row, err := s.store.UpsertPolicy(ctx, db.UpsertPolicyParams{
		ScopeType: string(scope.Type),
		ScopeID:   scope.ID,
		Kind:      string(policy.KindEnvironment),
		Selector:  selector,
		Rules:     rules,
		Locked:    in.Locked,
		Enabled:   true,
	})
	if err != nil {
		return EnvironmentPolicyView{}, fmt.Errorf("upsert environment policy: %w", err)
	}
	return environmentPolicyView(row)
}

// deleteEnvironmentPolicyAt removes an environment policy at one scope,
// returning ErrNotFound when no policy matched the selector.
func (s *Service) deleteEnvironmentPolicyAt(ctx context.Context, scope policyScope, selector string) error {
	rows, err := s.store.DeletePolicy(ctx, db.DeletePolicyParams{
		ScopeType: string(scope.Type),
		ScopeID:   scope.ID,
		Kind:      string(policy.KindEnvironment),
		Selector:  selector,
	})
	if err != nil {
		return fmt.Errorf("delete environment policy: %w", err)
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

// inheritedEnvironmentPolicies returns the policies a repo inherits from its
// project and tenant, ordered tenant first then project (broad -> narrow).
func (s *Service) inheritedEnvironmentPolicies(ctx context.Context, projectID, tenantID uuid.UUID) ([]EnvironmentPolicyView, error) {
	tenant, err := s.listEnvironmentPoliciesAt(ctx, policyScope{policy.ScopeTenant, tenantID})
	if err != nil {
		return nil, err
	}
	project, err := s.listEnvironmentPoliciesAt(ctx, policyScope{policy.ScopeProject, projectID})
	if err != nil {
		return nil, err
	}
	return append(tenant, project...), nil
}

// environmentPolicyView decodes a stored policy row into its API-facing view.
func environmentPolicyView(row db.Policy) (EnvironmentPolicyView, error) {
	rule, err := policy.DecodeEnvironmentRule(row.Rules)
	if err != nil {
		return EnvironmentPolicyView{}, fmt.Errorf("decode environment rule: %w", err)
	}
	return EnvironmentPolicyView{
		Scope:     policy.ScopeType(row.ScopeType),
		Selector:  row.Selector,
		Rule:      rule,
		Locked:    row.Locked,
		UpdatedAt: row.UpdatedAt,
	}, nil
}

// normalizeSources trims, drops empties, and de-duplicates the allowed-source
// globs so storage is stable and validation has nothing blank to choke on.
func normalizeSources(in []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(in))
	for _, raw := range in {
		s := strings.TrimSpace(raw)
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}
