package platform

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/nielsuitterdijk22/quill/internal/egress"
	"github.com/nielsuitterdijk22/quill/internal/store/db"
)

// Pipeline egress controls (2026-11-15, Forge's egress gateway made a
// built-in). An organization picks "allow all" or an allowlist that starts
// as egress.DefaultGroups; every repository inherits it and may add hosts.
// The policy is resolved at enqueue time into the JobSpec (like secrets) and
// enforced by the CI worker.

// TenantEgressView is an organization's policy as the settings page shows it.
type TenantEgressView struct {
	Tenant struct {
		Slug string `json:"slug"`
		Name string `json:"name"`
		// Personal: an account's own tenant (edited under account settings).
		Personal bool `json:"personal"`
	} `json:"tenant"`
	Mode string `json:"mode"`
	// Hosts is the organization allowlist (the defaults until customised).
	Hosts      []string       `json:"hosts"`
	Customized bool           `json:"customized"`
	Defaults   []egress.Group `json:"defaults"`
	// Required are always allowed (the platform itself, actions, runner
	// images).
	Required []string `json:"required"`
	CanEdit  bool     `json:"canEdit"`
}

// RepoEgressView is a repository's view: what it inherits plus its extras.
type RepoEgressView struct {
	Inherited TenantEgressView `json:"inherited"`
	// Hosts are this repository's additional hosts.
	Hosts []string `json:"hosts"`
}

// TenantEgressInput replaces an organization policy. Hosts nil resets the
// allowlist to the defaults.
type TenantEgressInput struct {
	Mode  string
	Hosts *[]string
}

// requiredEgressHosts are the hosts the platform adds to every allowlist:
// the git/api host jobs check out from, plus what egress.RequiredHosts
// always needs (the worker adds its own api host and runner registries).
func (s *Service) requiredEgressHosts() []string {
	return egress.RequiredHosts(s.gitPublicURL)
}

func (s *Service) tenantEgressView(ctx context.Context, tenant db.Tenant, canEdit bool) (TenantEgressView, error) {
	stored, err := s.store.GetTenantEgress(ctx, tenant.ID)
	if err != nil {
		return TenantEgressView{}, fmt.Errorf("load egress policy: %w", err)
	}
	v := TenantEgressView{Mode: egress.ModeAllowlist, Defaults: egress.DefaultGroups, Required: s.requiredEgressHosts(), CanEdit: canEdit}
	v.Tenant.Slug, v.Tenant.Name = tenant.Slug, tenant.Name
	if members, err := s.store.ListTenantMembers(ctx, tenant.ID); err == nil && len(members) == 0 {
		v.Tenant.Personal = true
	}
	if stored.AllowAll {
		v.Mode = egress.ModeAllowAll
	}
	v.Hosts = egress.Normalize(egress.DefaultHosts())
	if stored.Hosts != nil {
		v.Hosts, v.Customized = egress.Normalize(stored.Hosts), true
	}
	return v, nil
}

// canAdminTenantEgress: org admins (and platform admins), and the owner of
// a personal tenant — the account's own tenant, which has no membership
// rows (an org tenant always has its admins as members).
func (s *Service) canAdminTenantEgress(ctx context.Context, actor Actor, tenantID uuid.UUID) bool {
	if actor.TenantID == tenantID {
		if members, err := s.store.ListTenantMembers(ctx, tenantID); err == nil && len(members) == 0 {
			return true
		}
	}
	return s.authorizeTenantAdmin(ctx, actor, tenantID) == nil
}

// GetTenantEgress returns an organization's egress policy (members read).
func (s *Service) GetTenantEgress(ctx context.Context, actor Actor, tenantSlug string) (TenantEgressView, error) {
	tenant, err := s.getTenant(ctx, tenantSlug)
	if err != nil {
		return TenantEgressView{}, err
	}
	if err := s.authorizeTenantMember(ctx, actor, tenant.ID); err != nil {
		return TenantEgressView{}, err
	}
	return s.tenantEgressView(ctx, tenant, s.canAdminTenantEgress(ctx, actor, tenant.ID))
}

// SetTenantEgress replaces an organization's egress policy (admins).
func (s *Service) SetTenantEgress(ctx context.Context, actor Actor, tenantSlug string, in TenantEgressInput) (TenantEgressView, error) {
	tenant, err := s.getTenant(ctx, tenantSlug)
	if err != nil {
		return TenantEgressView{}, err
	}
	if !s.canAdminTenantEgress(ctx, actor, tenant.ID) {
		return TenantEgressView{}, ErrForbidden
	}
	if in.Mode != egress.ModeAllowlist && in.Mode != egress.ModeAllowAll {
		return TenantEgressView{}, fmt.Errorf("%w: mode must be %q or %q", ErrInvalidInput, egress.ModeAllowlist, egress.ModeAllowAll)
	}
	var hosts []string
	if in.Hosts != nil {
		if hosts, err = egress.ParseHosts(*in.Hosts); err != nil {
			return TenantEgressView{}, fmt.Errorf("%w: %v", ErrInvalidInput, err)
		}
	}
	if err := s.store.SetTenantEgress(ctx, tenant.ID, actor.UserID, in.Mode == egress.ModeAllowAll, hosts); err != nil {
		return TenantEgressView{}, fmt.Errorf("save egress policy: %w", err)
	}
	return s.tenantEgressView(ctx, tenant, true)
}

// MyTenantSlug returns the slug of the actor's own (personal) tenant.
func (s *Service) MyTenantSlug(ctx context.Context, actor Actor) (string, error) {
	t, err := s.store.GetTenantByID(ctx, actor.TenantID)
	if err != nil {
		return "", fmt.Errorf("load tenant: %w", err)
	}
	return t.Slug, nil
}

func (s *Service) repoTenant(ctx context.Context, repo db.Repository) (db.Tenant, error) {
	project, err := s.store.GetProjectByID(ctx, repo.ProjectID)
	if err != nil {
		return db.Tenant{}, fmt.Errorf("load project: %w", err)
	}
	return s.store.GetTenantByID(ctx, project.TenantID)
}

// GetRepoEgress returns a repository's inherited policy and extra hosts
// (project admins, like secrets).
func (s *Service) GetRepoEgress(ctx context.Context, actor Actor, projectSlug, repoSlug string) (RepoEgressView, error) {
	repo, err := s.authorizedRepo(ctx, actor, projectSlug, repoSlug)
	if err != nil {
		return RepoEgressView{}, err
	}
	return s.repoEgressView(ctx, actor, repo)
}

func (s *Service) repoEgressView(ctx context.Context, actor Actor, repo db.Repository) (RepoEgressView, error) {
	tenant, err := s.repoTenant(ctx, repo)
	if err != nil {
		return RepoEgressView{}, err
	}
	inherited, err := s.tenantEgressView(ctx, tenant, s.canAdminTenantEgress(ctx, actor, tenant.ID))
	if err != nil {
		return RepoEgressView{}, err
	}
	hosts, err := s.store.GetRepoEgressHosts(ctx, repo.ID)
	if err != nil {
		return RepoEgressView{}, fmt.Errorf("load repo egress hosts: %w", err)
	}
	return RepoEgressView{Inherited: inherited, Hosts: egress.Normalize(hosts)}, nil
}

// SetRepoEgress replaces a repository's extra hosts. A repository can only
// extend its organization's allowlist; "allow all" is an organization
// decision.
func (s *Service) SetRepoEgress(ctx context.Context, actor Actor, projectSlug, repoSlug string, hosts []string) (RepoEgressView, error) {
	repo, err := s.authorizedRepo(ctx, actor, projectSlug, repoSlug)
	if err != nil {
		return RepoEgressView{}, err
	}
	clean, err := egress.ParseHosts(hosts)
	if err != nil {
		return RepoEgressView{}, fmt.Errorf("%w: %v", ErrInvalidInput, err)
	}
	if err := s.store.SetRepoEgressHosts(ctx, repo.ID, actor.UserID, clean); err != nil {
		return RepoEgressView{}, fmt.Errorf("save repo egress hosts: %w", err)
	}
	return s.repoEgressView(ctx, actor, repo)
}

// resolveRunEgress is the policy a run of repo carries: allow all, or the
// required hosts + the organization allowlist + the repository's extras.
func (s *Service) resolveRunEgress(ctx context.Context, repo db.Repository) (*egress.Policy, error) {
	tenant, err := s.repoTenant(ctx, repo)
	if err != nil {
		return nil, err
	}
	stored, err := s.store.GetTenantEgress(ctx, tenant.ID)
	if err != nil {
		return nil, err
	}
	if stored.AllowAll {
		return &egress.Policy{Mode: egress.ModeAllowAll}, nil
	}
	org := stored.Hosts
	if org == nil {
		org = egress.DefaultHosts()
	}
	extra, err := s.store.GetRepoEgressHosts(ctx, repo.ID)
	if err != nil {
		return nil, err
	}
	hosts := append(append(s.requiredEgressHosts(), org...), extra...)
	return &egress.Policy{Mode: egress.ModeAllowlist, Hosts: egress.Normalize(hosts)}, nil
}
