package platform

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/nielsuitterdijk22/quill/internal/creem"
	"github.com/nielsuitterdijk22/quill/internal/store/db"
)

// Billing plans. 'free' is the individual tier with hard limits; 'team',
// 'enterprise' and 'internal' are vendor-provisioned plans without hard caps
// (usage-based invoicing / internal dogfooding).
const (
	PlanFree       = "free"
	PlanPro        = "pro" // the individual paid plan: a personal account's upgrade
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
	return s.billingView(ctx, tenant)
}

// GetPersonalBilling returns the billing view for the caller's own account
// tenant — the per-account tenant provisioned at signup for freemium gating
// and the personal Pro checkout. Users on the legacy shared tenant
// (pre-provisioning accounts) see that tenant's view, which carries the
// unlimited internal plan.
func (s *Service) GetPersonalBilling(ctx context.Context, actor Actor) (BillingView, error) {
	tenantID, err := s.personalTenantID(ctx, actor.UserID)
	if err != nil {
		return BillingView{}, err
	}
	tenant, err := s.store.GetTenantByID(ctx, tenantID)
	if err != nil {
		return BillingView{}, fmt.Errorf("lookup account tenant: %w", err)
	}
	return s.billingView(ctx, tenant)
}

// billingView meters a tenant's usage and attaches its plan's limits. Shared
// body of the org and personal billing views so every surface agrees.
func (s *Service) billingView(ctx context.Context, tenant db.Tenant) (BillingView, error) {
	repos, err := s.store.CountRepositoriesByTenant(ctx, tenant.ID)
	if err != nil {
		return BillingView{}, fmt.Errorf("count repositories: %w", err)
	}
	minutes, err := s.store.SumPipelineMinutesByTenant(ctx, tenant.ID)
	if err != nil {
		return BillingView{}, fmt.Errorf("sum pipeline minutes: %w", err)
	}

	storage, err := s.storageUsage(ctx, tenant.ID)
	if err != nil {
		return BillingView{}, err
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
	case PlanFree, PlanPro, PlanTeam, PlanEnterprise, PlanInternal:
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

// storageUsage sums the git-byte size of every repository in the tenant:
// engine repos from the referee rows, any remaining Forgejo repos from Forgejo. It mirrors GetBilling's metering so enforcement and the
// billing view always agree. Repos whose Forgejo linkage or API read fails
// count as zero bytes, matching the billing view's tolerant behavior.
func (s *Service) storageUsage(ctx context.Context, tenantID uuid.UUID) (int64, error) {
	// Engine repos (all repos since the D2 cutover): the referee's bytes.
	engineBytes, err := s.store.SumEngineStorageByTenant(ctx, tenantID)
	if err != nil {
		return 0, fmt.Errorf("sum engine storage: %w", err)
	}
	if !s.forgejoEnabled() {
		return engineBytes, nil
	}
	all, err := s.store.ListEngineRepos(ctx)
	if err != nil {
		return 0, fmt.Errorf("list engine repos: %w", err)
	}
	engineRepos := make(map[uuid.UUID]bool, len(all))
	for _, r := range all {
		engineRepos[r.RepoID] = true
	}
	projects, err := s.store.ListProjectsByTenantID(ctx, tenantID)
	if err != nil {
		return 0, fmt.Errorf("list projects: %w", err)
	}
	total := engineBytes
	for _, project := range projects {
		repos, err := s.store.ListRepositoriesByProject(ctx, db.ListRepositoriesByProjectParams{ProjectID: project.ID, Limit: 500, Offset: 0})
		if err != nil {
			return 0, fmt.Errorf("list repositories: %w", err)
		}
		for _, r := range repos {
			if !r.ForgejoOwner.Valid || !r.ForgejoName.Valid || engineRepos[r.ID] {
				continue
			}
			repo, err := s.forgejo.GetRepo(ctx, r.ForgejoOwner.String, r.ForgejoName.String)
			if err != nil {
				s.logger.Warn("storage metering: forgejo repo size unavailable", "repo", r.Slug, "error", err)
				continue
			}
			total += repo.Size
		}
	}
	return total, nil
}

// enforceStorageQuota blocks repository creation when the tenant's free-plan
// storage allotment is used up. The size probe goes through s.storageProbe so
// tests can override it without a live Forgejo; the production probe is
// storageUsage (Forgejo-backed, mirroring GetBilling's metering).
func (s *Service) enforceStorageQuota(ctx context.Context, tenantID uuid.UUID) error {
	tenant, err := s.store.GetTenantByID(ctx, tenantID)
	if err != nil {
		return fmt.Errorf("lookup tenant: %w", err)
	}
	if tenant.Plan != PlanFree {
		return nil
	}
	used, err := s.storageProbe(ctx, tenantID)
	if err != nil {
		return fmt.Errorf("measure storage: %w", err)
	}
	if used >= FreeMaxStorageBytes {
		return fmt.Errorf("%w: the free plan includes 1 GiB of git storage; contact us or upgrade your plan to keep growing", ErrLimitReached)
	}
	return nil
}

// StartTeamCheckout opens a Creem checkout session for the tenant's upgrade to
// the Team plan. Tenant-admin only (mirrors GetBilling), because starting a
// checkout is a billing action. successURL is the page the customer lands on
// after paying (the org's billing panel).
func (s *Service) StartTeamCheckout(ctx context.Context, actor Actor, orgSlug, successURL, requestID string) (string, error) {
	tenant, err := s.getTenant(ctx, orgSlug)
	if err != nil {
		return "", err
	}
	if err := s.authorizeTenantAdmin(ctx, actor, tenant.ID); err != nil {
		return "", err
	}
	if s.creem == nil || !s.creem.Enabled() || s.creem.TeamProductID() == "" {
		return "", ErrPaymentsNotConfigured
	}
	co, err := s.creem.CreateCheckout(ctx, s.creem.TeamProductID(), successURL, requestID, map[string]string{
		"tenantSlug": orgSlug,
	})
	if err != nil {
		return "", fmt.Errorf("creem checkout: %w", err)
	}
	return co.CheckoutURL, nil
}

// StartPersonalCheckout opens a Creem checkout session to upgrade the caller's
// personal account to the Pro plan. Self-service by definition: the caller's
// own account tenant is the target, so the authenticated actor is the entire
// authorization. Users whose accounts predate per-account provisioning sit on
// the legacy shared tenant (already unlimited) and get a clear error.
func (s *Service) StartPersonalCheckout(ctx context.Context, actor Actor, successURL, requestID string) (string, error) {
	user, err := s.store.GetUserByID(ctx, actor.UserID)
	if err != nil {
		return "", fmt.Errorf("lookup user: %w", err)
	}
	if !user.TenantID.Valid {
		return "", fmt.Errorf("%w: this account predates personal plans and is already unlimited", ErrInvalidInput)
	}
	if s.creem == nil || !s.creem.Enabled() || s.creem.ProProductID() == "" {
		return "", ErrPaymentsNotConfigured
	}
	tenant, err := s.store.GetTenantByID(ctx, user.TenantID.UUID)
	if err != nil {
		return "", fmt.Errorf("lookup account tenant: %w", err)
	}
	co, err := s.creem.CreateCheckout(ctx, s.creem.ProProductID(), successURL, requestID, map[string]string{
		"tenantSlug": tenant.Slug,
	})
	if err != nil {
		return "", fmt.Errorf("creem checkout: %w", err)
	}
	return co.CheckoutURL, nil
}

// CreemEvent is the subset of a Creem webhook payload Quill reacts to. Fields
// are parsed tolerantly (top-level metadata and obj.metadata both accepted) so
// schema drift in vendor payloads degrades to a no-op instead of a panic.
type CreemEvent struct {
	EventType string `json:"eventType"`
	Obj       struct {
		ID         string            `json:"id"`
		ProductID  string            `json:"product_id"`
		CustomerID string            `json:"customer_id"`
		Metadata   map[string]string `json:"metadata"`
	} `json:"obj"`
	Metadata map[string]string `json:"metadata"`
}

// ProcessCreemWebhook verifies the signature (if a webhook secret is
// configured) and applies checkout.completed events mapped to Quill plans. It
// returns the org slug whose plan changed, or "" for an ignored event. The plan
// change is executed as the platform (admin) actor — a verified webhook IS the
// vendor's instruction; usage overage and cancellations come later.
func (s *Service) ProcessCreemWebhook(ctx context.Context, body []byte, signatureHeader string) (string, error) {
	if s.creem == nil || !s.creem.WebhookConfigured() {
		return "", ErrPaymentsNotConfigured
	}
	if !creem.VerifySignature([]byte(s.creem.WebhookSecret()), body, signatureHeader, 10*time.Minute) {
		return "", fmt.Errorf("%w: invalid webhook signature", ErrInvalidInput)
	}
	var evt CreemEvent
	if err := json.Unmarshal(body, &evt); err != nil {
		return "", fmt.Errorf("%w: malformed webhook payload", ErrInvalidInput)
	}
	if evt.EventType != "checkout.completed" {
		return "", nil
	}
	tenantSlug := evt.Obj.Metadata["tenantSlug"]
	if tenantSlug == "" {
		tenantSlug = evt.Obj.Metadata["orgSlug"] // pre-personal-plan events
	}
	if tenantSlug == "" {
		tenantSlug = evt.Metadata["tenantSlug"]
	}
	if tenantSlug == "" {
		tenantSlug = evt.Metadata["orgSlug"]
	}
	if tenantSlug == "" {
		return "", nil
	}
	var plan string
	switch evt.Obj.ProductID {
	case s.creem.TeamProductID():
		plan = PlanTeam
	case s.creem.ProProductID():
		plan = PlanPro
	default:
		// Unknown product (e.g. a future enterprise SKU) — nothing to do yet.
		return "", nil
	}
	if _, err := s.SetTenantPlan(ctx, Actor{IsAdmin: true}, tenantSlug, plan); err != nil {
		return "", err
	}
	s.logger.Info("creem webhook: plan upgraded", "tenant", tenantSlug, "plan", plan, "product", evt.Obj.ProductID, "customer", evt.Obj.CustomerID)
	return tenantSlug, nil
}
