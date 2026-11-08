package platform

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/nielsuitterdijk22/quill/internal/store/db"
)

// Billing plans. 'free' is the individual tier with hard limits; 'team',
// 'enterprise' and 'internal' are vendor-provisioned plans without hard caps
// (usage-based invoicing / internal dogfooding).
const (
	PlanFree       = "free"
	PlanTeam       = "team"
	PlanEnterprise = "enterprise"
	PlanInternal   = "internal"
)

// Free-tier limits (individual tier): 50 repos, 1 GiB git storage,
// 100 pipeline minutes per month.
const (
	FreeMaxRepos           = 50
	FreeMaxStorageBytes    = 1 << 30 // 1 GiB
	FreeMaxPipelineMinutes = 100
)

// BillingLimits are the hard caps for a plan; nil fields mean unlimited.
type BillingLimits struct {
	Repos           *int64   `json:"repos,omitempty"`
	StorageBytes    *int64   `json:"storageBytes,omitempty"`
	PipelineMinutes *float64 `json:"pipelineMinutes,omitempty"`
}

// BillingUsage is the tenant's metered usage.
type BillingUsage struct {
	Repos           int64   `json:"repos"`
	StorageBytes    int64   `json:"storageBytes"`
	PipelineMinutes float64 `json:"pipelineMinutes"`
}

// BillingView is the API-facing billing surface for a tenant.
type BillingView struct {
	Plan   string         `json:"plan"`
	Usage  BillingUsage   `json:"usage"`
	Limits *BillingLimits `json:"limits,omitempty"`
}

// limitsForPlan maps a plan to its hard limits; nil means no hard caps.
func limitsForPlan(plan string) *BillingLimits {
	if plan != PlanFree {
		return nil
	}
	repos := int64(FreeMaxRepos)
	storage := int64(FreeMaxStorageBytes)
	minutes := float64(FreeMaxPipelineMinutes)
	return &BillingLimits{Repos: &repos, StorageBytes: &storage, PipelineMinutes: &minutes}
}

// GetBilling returns the tenant's plan and metered usage. Restricted to tenant
// admins (or platform admins): billing data must not be visible to ordinary
// members. Storage is metered from Forgejo repo sizes when the git backend is
// enabled; otherwise it reports 0 (DB-only deployments don't track git bytes).
func (s *Service) GetBilling(ctx context.Context, actor Actor, orgSlug string) (BillingView, error) {
	tenant, err := s.getTenant(ctx, orgSlug)
	if err != nil {
		return BillingView{}, err
	}
	if err := s.authorizeTenantAdmin(ctx, actor, tenant.ID); err != nil {
		return BillingView{}, err
	}

	repos, err := s.store.CountRepositoriesByTenant(ctx, tenant.ID)
	if err != nil {
		return BillingView{}, fmt.Errorf("count repositories: %w", err)
	}
	minutes, err := s.store.SumPipelineMinutesByTenant(ctx, tenant.ID)
	if err != nil {
		return BillingView{}, fmt.Errorf("sum pipeline minutes: %w", err)
	}

	var storage int64
	if s.forgejoEnabled() {
		projects, err := s.store.ListProjectsByTenant(ctx, tenant.ID)
		if err != nil {
			return BillingView{}, fmt.Errorf("list projects: %w", err)
		}
		for _, p := range projects {
			repos, err := s.store.ListRepositoriesByProject(ctx, db.ListRepositoriesByProjectParams{ProjectID: p.ID, Limit: 500, Offset: 0})
			if err != nil {
				return BillingView{}, fmt.Errorf("list repositories: %w", err)
			}
			for _, r := range repos {
				if !r.ForgejoOwner.Valid || !r.ForgejoName.Valid {
					continue
				}
				repo, err := s.forgejo.GetRepo(ctx, r.ForgejoOwner.String, r.ForgejoName.String)
				if err != nil {
					// A missing Forgejo repo still counts as 0 bytes; don't fail billing.
					s.logger.Warn("billing: forgejo repo size unavailable", "repo", r.Slug, "error", err)
					continue
				}
				storage += repo.Size
			}
		}
	}

	return BillingView{
		Plan:   tenant.Plan,
		Usage:  BillingUsage{Repos: repos, StorageBytes: storage, PipelineMinutes: minutes},
		Limits: limitsForPlan(tenant.Plan),
	}, nil
}

// SetTenantPlan changes the tenant's plan. Platform-admin only (the vendor
// provisions plans; org admins see them). The change is expected to be
// audit-logged by the caller.
func (s *Service) SetTenantPlan(ctx context.Context, actor Actor, orgSlug, plan string) (db.Tenant, error) {
	if err := s.authorizePlatformAdmin(actor); err != nil {
		return db.Tenant{}, err
	}
	switch plan {
	case PlanFree, PlanTeam, PlanEnterprise, PlanInternal:
	default:
		return db.Tenant{}, fmt.Errorf("%w: plan must be one of free|team|enterprise|internal", ErrInvalidInput)
	}
	tenant, err := s.getTenant(ctx, orgSlug)
	if err != nil {
		return db.Tenant{}, err
	}
	if err := s.store.SetTenantPlan(ctx, db.SetTenantPlanParams{ID: tenant.ID, Plan: plan}); err != nil {
		return db.Tenant{}, fmt.Errorf("set tenant plan: %w", err)
	}
	tenant.Plan = plan
	return tenant, nil
}

// enforceRepoQuota rejects repository creation when the tenant's free-plan repo
// allotment is used up. Paid plans (team/enterprise/internal) are unlimited, so
// the gate is a no-op for them.
func (s *Service) enforceRepoQuota(ctx context.Context, tenantID uuid.UUID) error {
	tenant, err := s.store.GetTenantByID(ctx, tenantID)
	if err != nil {
		return fmt.Errorf("lookup tenant: %w", err)
	}
	if tenant.Plan != PlanFree {
		return nil
	}
	repos, err := s.store.CountRepositoriesByTenant(ctx, tenant.ID)
	if err != nil {
		return fmt.Errorf("count repositories: %w", err)
	}
	if repos >= FreeMaxRepos {
		return fmt.Errorf("%w: the free plan includes %d repositories; contact us or upgrade your plan to add more", ErrLimitReached, FreeMaxRepos)
	}
	return nil
}

// enforcePipelineMinutesQuota rejects workflow triggers once the tenant's
// finished-run minutes this calendar month reach the free-plan allowance.
// Metering counts finished runs only, matching what GetBilling reports.
func (s *Service) enforcePipelineMinutesQuota(ctx context.Context, tenantID uuid.UUID) error {
	tenant, err := s.store.GetTenantByID(ctx, tenantID)
	if err != nil {
		return fmt.Errorf("lookup tenant: %w", err)
	}
	if tenant.Plan != PlanFree {
		return nil
	}
	minutes, err := s.store.SumPipelineMinutesByTenant(ctx, tenant.ID)
	if err != nil {
		return fmt.Errorf("sum pipeline minutes: %w", err)
	}
	if minutes >= float64(FreeMaxPipelineMinutes) {
		return fmt.Errorf("%w: the free plan includes %d pipeline minutes per month; contact us or upgrade your plan to keep building", ErrLimitReached, FreeMaxPipelineMinutes)
	}
	return nil
}
