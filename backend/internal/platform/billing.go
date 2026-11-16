package platform

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/nielsuitterdijk22/quill/internal/creem"
	"github.com/nielsuitterdijk22/quill/internal/store/db"
)

// Billing plans. 'free' is the individual tier with hard limits; 'team',
// 'enterprise' and 'internal' are vendor-provisioned plans without hard caps
// (usage-based invoicing / internal dogfooding). Organizations are not free:
// a new org runs a 14-day Team 'trial', and an org whose trial ended or whose
// subscription was canceled is 'lapsed' (locked: data stays readable, new
// repos/pipelines/invites are refused until it subscribes).
const (
	PlanFree       = "free"
	PlanPro        = "pro" // the individual paid plan: a personal account's upgrade
	PlanTeam       = "team"
	PlanEnterprise = "enterprise"
	PlanInternal   = "internal"
	PlanTrial      = "trial"
	PlanLapsed     = "lapsed"
)

// TrialDays is the length of a new organization's Team trial.
const TrialDays = 14

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
	// TrialEndsAt is set while (or after) the tenant ran a trial.
	TrialEndsAt *time.Time `json:"trialEndsAt,omitempty"`
	// Locked reports that the tenant needs a paid subscription before it can
	// create repositories, run pipelines or invite members.
	Locked bool `json:"locked"`
	// Seats is the member count a Team checkout charges for.
	Seats int `json:"seats"`
}

// tenantLocked reports whether a tenant's plan currently refuses new work: a
// lapsed subscription, or a trial whose window has passed (a trial without a
// recorded window is treated as ended — orgs are not free).
func (s *Service) tenantLocked(ctx context.Context, tenant db.Tenant) (bool, *time.Time, error) {
	switch tenant.Plan {
	case PlanLapsed:
		sub, err := s.store.GetTenantSubscription(ctx, tenant.ID)
		if err != nil {
			return false, nil, fmt.Errorf("load subscription: %w", err)
		}
		return true, sub.TrialEndsAt, nil
	case PlanTrial:
		sub, err := s.store.GetTenantSubscription(ctx, tenant.ID)
		if err != nil {
			return false, nil, fmt.Errorf("load subscription: %w", err)
		}
		if sub.TrialEndsAt == nil || !s.now().Before(*sub.TrialEndsAt) {
			return true, sub.TrialEndsAt, nil
		}
		return false, sub.TrialEndsAt, nil
	}
	return false, nil, nil
}

// enforceTenantActive refuses new work for a locked (unpaid) organization.
func (s *Service) enforceTenantActive(ctx context.Context, tenant db.Tenant) error {
	locked, _, err := s.tenantLocked(ctx, tenant)
	if err != nil {
		return err
	}
	if !locked {
		return nil
	}
	if tenant.Plan == PlanLapsed {
		return fmt.Errorf("%w: this organization's Team subscription has ended; subscribe again from the organization's billing settings to keep building (existing repositories stay readable)", ErrLimitReached)
	}
	return fmt.Errorf("%w: this organization's %d-day Team trial has ended; subscribe from the organization's billing settings to keep building (existing repositories stay readable)", ErrLimitReached, TrialDays)
}

// StartOrgTrial puts a freshly created organization on a TrialDays trial.
func (s *Service) startOrgTrial(ctx context.Context, tenantID uuid.UUID) error {
	return s.store.StartTenantTrial(ctx, tenantID, s.now().Add(TrialDays*24*time.Hour))
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

	locked, trialEnds, err := s.tenantLocked(ctx, tenant)
	if err != nil {
		return BillingView{}, err
	}
	members, err := s.store.ListTenantMembers(ctx, tenant.ID)
	if err != nil {
		return BillingView{}, fmt.Errorf("count members: %w", err)
	}
	return BillingView{
		Plan:        tenant.Plan,
		Usage:       BillingUsage{Repos: repos, StorageBytes: storage, PipelineMinutes: minutes},
		Limits:      limitsForPlan(tenant.Plan),
		TrialEndsAt: trialEnds,
		Locked:      locked,
		Seats:       max(1, len(members)),
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
	case PlanFree, PlanPro, PlanTeam, PlanEnterprise, PlanInternal, PlanTrial, PlanLapsed:
	default:
		return db.Tenant{}, fmt.Errorf("%w: plan must be one of free|pro|team|enterprise|internal|trial|lapsed", ErrInvalidInput)
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
	if err := s.enforceTenantActive(ctx, tenant); err != nil {
		return err
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
	if err := s.enforceTenantActive(ctx, tenant); err != nil {
		return err
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

// StartTeamCheckout opens a Creem checkout session for the organization's
// Team subscription (from a trial, a lapsed plan, or a legacy free org).
// Tenant-admin only (mirrors GetBilling), because starting a checkout is a
// billing action. Team is priced per seat, so the session carries the org's
// current member count; successURL is where the customer lands after paying.
func (s *Service) StartTeamCheckout(ctx context.Context, actor Actor, orgSlug, successURL, requestID string) (string, error) {
	tenant, err := s.getTenant(ctx, orgSlug)
	if err != nil {
		return "", err
	}
	if err := s.authorizeTenantAdmin(ctx, actor, tenant.ID); err != nil {
		return "", err
	}
	switch tenant.Plan {
	case PlanTeam, PlanEnterprise, PlanInternal:
		return "", fmt.Errorf("%w: this organization already has a %s plan", ErrConflict, tenant.Plan)
	}
	if s.creem == nil || !s.creem.Enabled() || s.creem.TeamProductID() == "" {
		return "", ErrPaymentsNotConfigured
	}
	members, err := s.store.ListTenantMembers(ctx, tenant.ID)
	if err != nil {
		return "", fmt.Errorf("count members: %w", err)
	}
	co, err := s.creem.CreateCheckout(ctx, creem.CheckoutInput{
		ProductID:     s.creem.TeamProductID(),
		SuccessURL:    successURL,
		RequestID:     requestID,
		Units:         len(members),
		CustomerEmail: s.actorEmail(ctx, actor),
		Metadata:      map[string]string{"tenantSlug": tenant.Slug},
	})
	if err != nil {
		return "", fmt.Errorf("creem checkout: %w", err)
	}
	return co.CheckoutURL, nil
}

// actorEmail is the caller's email for pre-filling checkouts ("" if unknown).
func (s *Service) actorEmail(ctx context.Context, actor Actor) string {
	if actor.UserID == uuid.Nil {
		return ""
	}
	u, err := s.store.GetUserByID(ctx, actor.UserID)
	if err != nil {
		return ""
	}
	return u.Email
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
	co, err := s.creem.CreateCheckout(ctx, creem.CheckoutInput{
		ProductID:     s.creem.ProProductID(),
		SuccessURL:    successURL,
		RequestID:     requestID,
		Units:         1,
		CustomerEmail: user.Email,
		Metadata:      map[string]string{"tenantSlug": tenant.Slug},
	})
	if err != nil {
		return "", fmt.Errorf("creem checkout: %w", err)
	}
	return co.CheckoutURL, nil
}

// CreemResult is what a processed webhook changed (zero value: ignored).
type CreemResult struct {
	TenantSlug string
	Plan       string // the plan the tenant is on now
	Upgraded   bool   // a paid plan was granted (vs. a lapse/downgrade)
}

// Creem event types Quill acts on.
var (
	creemGrantEvents  = map[string]bool{"checkout.completed": true, "subscription.active": true, "subscription.paid": true, "subscription.trialing": true}
	creemRevokeEvents = map[string]bool{"subscription.canceled": true, "subscription.expired": true}
)

// ProcessCreemWebhook verifies a Creem webhook (creem-signature = hex HMAC of
// the raw body) and applies it:
//   - checkout.completed / subscription.active|paid|trialing grant the plan the
//     product sells (Team for orgs, Pro for personal accounts) and record the
//     Creem customer/subscription ids;
//   - subscription.canceled / expired lapse a Team org (locked, data kept) or
//     return a Pro account to free — but only for the subscription currently
//     on record, so a stale cancel can't revoke a newer subscription.
//
// The tenant comes from the checkout metadata (tenantSlug; orgSlug for
// pre-personal-plan sessions), else from the recorded subscription id.
// Every action is idempotent, so Creem's retries and replays are harmless.
func (s *Service) ProcessCreemWebhook(ctx context.Context, body []byte, signatureHeader string) (CreemResult, error) {
	if s.creem == nil || !s.creem.WebhookConfigured() {
		return CreemResult{}, ErrPaymentsNotConfigured
	}
	if !creem.VerifySignature([]byte(s.creem.WebhookSecret()), body, signatureHeader) {
		return CreemResult{}, fmt.Errorf("%w: invalid webhook signature", ErrInvalidInput)
	}
	evt, err := creem.ParseEvent(body)
	if err != nil {
		return CreemResult{}, fmt.Errorf("%w: malformed webhook payload", ErrInvalidInput)
	}
	grant, revoke := creemGrantEvents[evt.Type], creemRevokeEvents[evt.Type]
	if !grant && !revoke {
		return CreemResult{}, nil
	}

	var paidPlan, lapsedPlan string
	switch {
	case evt.ProductID != "" && evt.ProductID == s.creem.TeamProductID():
		paidPlan, lapsedPlan = PlanTeam, PlanLapsed
	case evt.ProductID != "" && evt.ProductID == s.creem.ProProductID():
		paidPlan, lapsedPlan = PlanPro, PlanFree
	default:
		// Unknown product (e.g. a future enterprise SKU) — nothing to do yet.
		return CreemResult{}, nil
	}

	tenant, ok, err := s.creemTenant(ctx, evt)
	if err != nil || !ok {
		return CreemResult{}, err
	}
	sub, err := s.store.GetTenantSubscription(ctx, tenant.ID)
	if err != nil {
		return CreemResult{}, fmt.Errorf("load subscription: %w", err)
	}

	if revoke {
		if sub.CreemSubscriptionID != "" && evt.SubscriptionID != "" && sub.CreemSubscriptionID != evt.SubscriptionID {
			s.logger.Info("creem webhook: ignoring cancel of a superseded subscription", "tenant", tenant.Slug, "subscription", evt.SubscriptionID)
			return CreemResult{}, nil
		}
		if tenant.Plan != paidPlan {
			return CreemResult{}, nil // already not on the plan this product sells
		}
		if _, err := s.SetTenantPlan(ctx, Actor{IsAdmin: true}, tenant.Slug, lapsedPlan); err != nil {
			return CreemResult{}, err
		}
		s.logger.Info("creem webhook: plan lapsed", "tenant", tenant.Slug, "plan", lapsedPlan, "event", evt.Type, "subscription", evt.SubscriptionID)
		return CreemResult{TenantSlug: tenant.Slug, Plan: lapsedPlan}, nil
	}

	// Grant. Vendor-provisioned plans (enterprise/internal) are never
	// overwritten by a self-service purchase.
	if tenant.Plan == PlanEnterprise || tenant.Plan == PlanInternal {
		s.logger.Warn("creem webhook: payment for a vendor-provisioned tenant left its plan unchanged", "tenant", tenant.Slug, "plan", tenant.Plan)
		return CreemResult{}, nil
	}
	if err := s.store.SetTenantCreemIDs(ctx, tenant.ID, evt.CustomerID, evt.SubscriptionID); err != nil {
		return CreemResult{}, fmt.Errorf("record creem ids: %w", err)
	}
	if tenant.Plan == paidPlan {
		return CreemResult{TenantSlug: tenant.Slug, Plan: paidPlan}, nil
	}
	if _, err := s.SetTenantPlan(ctx, Actor{IsAdmin: true}, tenant.Slug, paidPlan); err != nil {
		return CreemResult{}, err
	}
	s.logger.Info("creem webhook: plan upgraded", "tenant", tenant.Slug, "plan", paidPlan, "event", evt.Type, "product", evt.ProductID, "customer", evt.CustomerID)
	return CreemResult{TenantSlug: tenant.Slug, Plan: paidPlan, Upgraded: true}, nil
}

// creemTenant resolves the tenant an event is about.
func (s *Service) creemTenant(ctx context.Context, evt creem.Event) (db.Tenant, bool, error) {
	slug := evt.Metadata["tenantSlug"]
	if slug == "" {
		slug = evt.Metadata["orgSlug"]
	}
	if slug != "" {
		t, err := s.store.GetTenantBySlug(ctx, slug)
		if errors.Is(err, pgx.ErrNoRows) {
			s.logger.Warn("creem webhook: unknown tenant in metadata", "tenant", slug, "event", evt.Type)
			return db.Tenant{}, false, nil
		}
		if err != nil {
			return db.Tenant{}, false, fmt.Errorf("lookup tenant: %w", err)
		}
		return t, true, nil
	}
	id, ok, err := s.store.TenantIDByCreemSubscription(ctx, evt.SubscriptionID)
	if err != nil || !ok {
		return db.Tenant{}, false, err
	}
	t, err := s.store.GetTenantByID(ctx, id)
	if err != nil {
		return db.Tenant{}, false, fmt.Errorf("lookup tenant: %w", err)
	}
	return t, true, nil
}
