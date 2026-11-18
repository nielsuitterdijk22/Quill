package platform

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/nielsuitterdijk22/quill/internal/creem"
	"github.com/nielsuitterdijk22/quill/internal/store/db"
)

// Billing plans. Quill has no free tier and no trial (2026-11-18): every
// account pays, except hand-selected beta users.
//
//   - 'starter' (EUR 4) and 'pro' (EUR 9) are the individual paid plans, with
//     hard storage / CI-minute caps; 'team' (EUR 11 per user) is the
//     organization plan, and 'enterprise' / 'internal' are vendor-provisioned
//     without caps.
//   - 'beta' is granted by the vendor (a beta invite link or the admin plan
//     switch) and carries Pro's limits at no charge.
//   - 'inactive' is where every new account starts and 'lapsed' is where a
//     canceled subscription lands: both are locked (data stays readable; new
//     repos, pipelines and invites are refused until the account subscribes).
const (
	PlanStarter    = "starter"
	PlanPro        = "pro"
	PlanTeam       = "team"
	PlanEnterprise = "enterprise"
	PlanInternal   = "internal"
	PlanBeta       = "beta"
	PlanInactive   = "inactive"
	PlanLapsed     = "lapsed"
)

// Plan limits (see the pricing page). Repository counts are not capped on any
// plan; storage and monthly CI minutes are.
const (
	StarterMaxStorageBytes    = 5 << 30  // 5 GiB
	StarterMaxPipelineMinutes = 100      // per month
	ProMaxStorageBytes        = 10 << 30 // 10 GiB
	ProMaxPipelineMinutes     = 500      // per month
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
	// Locked reports that the tenant needs a subscription (or a beta invite)
	// before it can create repositories, run pipelines or invite members.
	Locked bool `json:"locked"`
	// Seats is the member count a Team checkout charges for.
	Seats int `json:"seats"`
	// BilledSeats is the seat count confirmed on the Creem subscription (Team
	// plans with a subscription; 0 otherwise).
	BilledSeats int `json:"billedSeats"`
}

// planLocked reports whether a plan currently refuses new work: an account
// that never subscribed ('inactive') or whose subscription ended ('lapsed').
func planLocked(plan string) bool {
	return plan == PlanInactive || plan == PlanLapsed
}

// enforceTenantActive refuses new work for a locked (unsubscribed) tenant.
func (s *Service) enforceTenantActive(_ context.Context, tenant db.Tenant) error {
	switch tenant.Plan {
	case PlanLapsed:
		return fmt.Errorf("%w: this subscription has ended; subscribe again from the billing settings to keep building (existing repositories stay readable)", ErrLimitReached)
	case PlanInactive:
		return fmt.Errorf("%w: Quill has no free plan; choose a plan in the billing settings to start building (existing repositories stay readable)", ErrLimitReached)
	}
	return nil
}

// limitsForPlan maps a plan to its hard limits; nil means no hard caps.
func limitsForPlan(plan string) *BillingLimits {
	var storage int64
	var minutes float64
	switch plan {
	case PlanStarter:
		storage, minutes = StarterMaxStorageBytes, StarterMaxPipelineMinutes
	case PlanPro, PlanBeta:
		storage, minutes = ProMaxStorageBytes, ProMaxPipelineMinutes
	default:
		return nil
	}
	return &BillingLimits{StorageBytes: &storage, PipelineMinutes: &minutes}
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

	members, err := s.store.ListTenantMembers(ctx, tenant.ID)
	if err != nil {
		return BillingView{}, fmt.Errorf("count members: %w", err)
	}
	var billed int
	if tenant.Plan == PlanTeam {
		sub, err := s.store.GetTenantSubscription(ctx, tenant.ID)
		if err != nil {
			return BillingView{}, fmt.Errorf("load subscription: %w", err)
		}
		if sub.CreemSubscriptionID != "" && sub.CreemSeats != max(1, len(members)) {
			// A previous sync failed (Creem unreachable): heal it now.
			s.syncTeamSeatsBestEffort(ctx, tenant.ID)
			if sub, err = s.store.GetTenantSubscription(ctx, tenant.ID); err != nil {
				return BillingView{}, fmt.Errorf("load subscription: %w", err)
			}
		}
		billed = sub.CreemSeats
	}
	return BillingView{
		BilledSeats: billed,
		Plan:        tenant.Plan,
		Usage:       BillingUsage{Repos: repos, StorageBytes: storage, PipelineMinutes: minutes},
		Limits:      limitsForPlan(tenant.Plan),
		Locked:      planLocked(tenant.Plan),
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
	case PlanStarter, PlanPro, PlanTeam, PlanEnterprise, PlanInternal, PlanBeta, PlanInactive, PlanLapsed:
	default:
		return db.Tenant{}, fmt.Errorf("%w: plan must be one of starter|pro|team|enterprise|internal|beta|inactive|lapsed", ErrInvalidInput)
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

// enforceRepoQuota rejects repository creation for a locked tenant. No plan
// caps the repository count (storage is the cap), so this is the lock check.
func (s *Service) enforceRepoQuota(ctx context.Context, tenantID uuid.UUID) error {
	tenant, err := s.store.GetTenantByID(ctx, tenantID)
	if err != nil {
		return fmt.Errorf("lookup tenant: %w", err)
	}
	return s.enforceTenantActive(ctx, tenant)
}

// enforcePipelineMinutesQuota rejects workflow triggers for a locked tenant,
// or once the finished-run minutes this calendar month reach the plan's CI
// allowance. Metering counts finished runs only, matching GetBilling.
func (s *Service) enforcePipelineMinutesQuota(ctx context.Context, tenantID uuid.UUID) error {
	tenant, err := s.store.GetTenantByID(ctx, tenantID)
	if err != nil {
		return fmt.Errorf("lookup tenant: %w", err)
	}
	if err := s.enforceTenantActive(ctx, tenant); err != nil {
		return err
	}
	limits := limitsForPlan(tenant.Plan)
	if limits == nil || limits.PipelineMinutes == nil {
		return nil
	}
	minutes, err := s.store.SumPipelineMinutesByTenant(ctx, tenant.ID)
	if err != nil {
		return fmt.Errorf("sum pipeline minutes: %w", err)
	}
	if minutes >= *limits.PipelineMinutes {
		return fmt.Errorf("%w: the %s plan includes %d CI minutes per month; upgrade your plan to keep building", ErrLimitReached, tenant.Plan, int(*limits.PipelineMinutes))
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

// enforceStorageQuota blocks repository creation when the tenant's plan
// storage allotment is used up. The size probe goes through s.storageProbe so
// tests can override it without a live Forgejo; the production probe is
// storageUsage.
func (s *Service) enforceStorageQuota(ctx context.Context, tenantID uuid.UUID) error {
	tenant, err := s.store.GetTenantByID(ctx, tenantID)
	if err != nil {
		return fmt.Errorf("lookup tenant: %w", err)
	}
	limits := limitsForPlan(tenant.Plan)
	if limits == nil || limits.StorageBytes == nil {
		return nil
	}
	used, err := s.storageProbe(ctx, tenantID)
	if err != nil {
		return fmt.Errorf("measure storage: %w", err)
	}
	if used >= *limits.StorageBytes {
		return fmt.Errorf("%w: the %s plan includes %d GiB of git storage; upgrade your plan to keep growing", ErrLimitReached, tenant.Plan, *limits.StorageBytes>>30)
	}
	return nil
}

// StartTeamCheckout opens a Creem checkout session for the organization's
// Team Pro subscription (from an inactive or lapsed organization).
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
// personal account to the given plan (PlanStarter or PlanPro). Self-service by definition: the caller's
// own account tenant is the target, so the authenticated actor is the entire
// authorization. Users whose accounts predate per-account provisioning sit on
// the legacy shared tenant (already unlimited) and get a clear error.
func (s *Service) StartPersonalCheckout(ctx context.Context, actor Actor, plan, successURL, requestID string) (string, error) {
	var productID string
	switch {
	case s.creem == nil:
	case plan == PlanStarter:
		productID = s.creem.StarterProductID()
	case plan == PlanPro:
		productID = s.creem.ProProductID()
	default:
		return "", fmt.Errorf("%w: plan must be %s or %s", ErrInvalidInput, PlanStarter, PlanPro)
	}
	user, err := s.store.GetUserByID(ctx, actor.UserID)
	if err != nil {
		return "", fmt.Errorf("lookup user: %w", err)
	}
	if !user.TenantID.Valid {
		return "", fmt.Errorf("%w: this account predates personal plans and is already unlimited", ErrInvalidInput)
	}
	if s.creem == nil || !s.creem.Enabled() || productID == "" {
		return "", ErrPaymentsNotConfigured
	}
	tenant, err := s.store.GetTenantByID(ctx, user.TenantID.UUID)
	if err != nil {
		return "", fmt.Errorf("lookup account tenant: %w", err)
	}
	co, err := s.creem.CreateCheckout(ctx, creem.CheckoutInput{
		ProductID:     productID,
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
//     product sells (Team Pro for orgs, Starter or Pro for personal accounts)
//     and record the
//     Creem customer/subscription ids;
//   - subscription.canceled / expired lapse a Team org (locked, data kept) or
//     (any paid plan) lapse the tenant (locked, data kept) — but only for the subscription currently
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
		paidPlan, lapsedPlan = PlanPro, PlanLapsed
	case evt.ProductID != "" && evt.ProductID == s.creem.StarterProductID():
		paidPlan, lapsedPlan = PlanStarter, PlanLapsed
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
		if paidPlan == PlanTeam {
			s.syncTeamSeatsBestEffort(ctx, tenant.ID)
		}
		return CreemResult{TenantSlug: tenant.Slug, Plan: paidPlan}, nil
	}
	if _, err := s.SetTenantPlan(ctx, Actor{IsAdmin: true}, tenant.Slug, paidPlan); err != nil {
		return CreemResult{}, err
	}
	if paidPlan == PlanTeam {
		// Members may have joined between checkout and payment.
		s.syncTeamSeatsBestEffort(ctx, tenant.ID)
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
