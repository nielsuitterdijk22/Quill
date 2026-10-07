package platform

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nielsuitterdijk22/quill/internal/store/db"
	"github.com/nielsuitterdijk22/quill/internal/zitadel"
)

// Organization SSO linking.
//
// Quill does NOT act as an OIDC relying party or provision identity providers
// itself. Self-service users all live in the default Zitadel org. For a customer
// that needs SSO, a platform engineer creates a dedicated Zitadel organization by
// hand — its IdP, verified email domain, and login policy live in the Zitadel
// console — and then links that Zitadel org to this Quill organization here.
//
// At login, Zitadel's own domain discovery routes the customer's users to their
// org/IdP; the issued token carries that org as its resource-owner claim, which
// the auth layer maps back to this Quill tenant (tenants.external_org_id) and
// grants membership (see auth.ZitadelVerifier). So all this surface stores is the
// Zitadel org id (the link) plus the email domain for the operator's reference —
// no secrets, no provisioning, no API calls.
//
// Configuration is restricted to platform admins ("no self-service"): an org
// admin cannot link their own org.

// ssoMaxField bounds the free-text SSO fields.
const ssoMaxField = 500

// SSOConfigInput is the desired SSO link for an organization.
type SSOConfigInput struct {
	// ExternalOrgID is the Zitadel organization id the customer's users
	// authenticate against.
	ExternalOrgID string
	// EmailDomain is stored for the operator's reference/display only — routing is
	// Zitadel's job, not Quill's.
	EmailDomain string
}

// SSOConfigView is an organization's SSO link in API-facing form.
type SSOConfigView struct {
	Configured    bool
	ExternalOrgID string
	EmailDomain   string
	UpdatedAt     time.Time

	// Domain-ownership verification. The verified flag is live-read from Zitadel
	// (the source of truth); the record fields are the persisted challenge to show
	// the operator. DomainStatusKnown is false when the status couldn't be read
	// (Zitadel unconfigured/unreachable), so the UI can say "unknown" rather than
	// falsely "pending".
	DomainVerified    bool
	DomainStatusKnown bool
	DomainRecordType  string // "dns" | "http" | "" (none generated yet)
	DomainRecordName  string
	DomainRecordValue string
}

// GetOrgSSO returns an organization's SSO link (platform admin only). When none
// is configured it returns a zero view with Configured=false.
func (s *Service) GetOrgSSO(ctx context.Context, actor Actor, orgSlug string) (SSOConfigView, error) {
	if err := s.authorizePlatformAdmin(actor); err != nil {
		return SSOConfigView{}, err
	}
	tenant, err := s.getTenant(ctx, orgSlug)
	if err != nil {
		return SSOConfigView{}, err
	}
	return s.ssoViewForTenant(ctx, tenant)
}

// SetOrgSSO links an organization to an existing Zitadel org (platform admin
// only). It performs no identity-provider provisioning — the Zitadel org, IdP,
// and domain are set up by hand in the Zitadel console.
func (s *Service) SetOrgSSO(ctx context.Context, actor Actor, orgSlug string, in SSOConfigInput) (SSOConfigView, error) {
	if err := s.authorizePlatformAdmin(actor); err != nil {
		return SSOConfigView{}, err
	}
	tenant, err := s.getTenant(ctx, orgSlug)
	if err != nil {
		return SSOConfigView{}, err
	}

	externalOrgID := strings.TrimSpace(in.ExternalOrgID)
	emailDomain := strings.ToLower(strings.TrimSpace(in.EmailDomain))
	for _, f := range []string{externalOrgID, emailDomain} {
		if len(f) > ssoMaxField {
			return SSOConfigView{}, fmt.Errorf("%w: an SSO field is too long", ErrInvalidInput)
		}
	}
	if externalOrgID == "" {
		return SSOConfigView{}, fmt.Errorf("%w: a Zitadel organization id is required", ErrInvalidInput)
	}
	if strings.ContainsAny(externalOrgID, " /@") {
		return SSOConfigView{}, fmt.Errorf("%w: the Zitadel organization id looks invalid", ErrInvalidInput)
	}
	if emailDomain != "" && (strings.ContainsAny(emailDomain, " @/") || !strings.Contains(emailDomain, ".")) {
		return SSOConfigView{}, fmt.Errorf("%w: email domain must be a bare domain like acme.com", ErrInvalidInput)
	}

	// The resource-owner -> tenant mapping must be one-to-one: reject a Zitadel org
	// already linked to a different Quill org, or a login from it would be
	// ambiguous. GetOrCreate is the only lookup available; it is safe here because
	// a hit returns the existing row and a miss creates the mapping we are about to
	// persist anyway (to this same tenant's slug it will be replaced below).
	if owner, err := s.store.GetTenantByExternalOrg(ctx, externalOrgID); err == nil && owner.ID != tenant.ID {
		return SSOConfigView{}, fmt.Errorf("%w: that Zitadel organization is already linked to another organization", ErrInvalidInput)
	} else if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return SSOConfigView{}, fmt.Errorf("check zitadel org link: %w", err)
	}

	// A domain change invalidates any previously generated challenge (it was issued
	// for the old domain), so remember the prior domain to decide whether to clear.
	var priorDomain string
	if existing, err := s.store.GetTenantSSO(ctx, tenant.ID); err == nil {
		priorDomain = existing.EmailDomain
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return SSOConfigView{}, fmt.Errorf("load sso config: %w", err)
	}

	if err := s.store.SetTenantExternalOrg(ctx, db.SetTenantExternalOrgParams{
		ID:            tenant.ID,
		ExternalOrgID: pgtype.Text{String: externalOrgID, Valid: true},
	}); err != nil {
		return SSOConfigView{}, fmt.Errorf("link zitadel org: %w", err)
	}
	// Store the reference metadata (email domain). Protocol keeps the table's
	// non-null default; the issuer/client/secret columns are unused now that Quill
	// no longer acts as a relying party.
	if _, err := s.store.UpsertTenantSSO(ctx, db.UpsertTenantSSOParams{
		TenantID:    tenant.ID,
		Protocol:    "oidc",
		EmailDomain: emailDomain,
		Enabled:     true,
	}); err != nil {
		// A partial unique index (migration 000020) keeps one email domain from
		// being claimed by two linked orgs.
		if isUniqueViolation(err) {
			return SSOConfigView{}, fmt.Errorf("%w: that email domain is already used by another organization", ErrInvalidInput)
		}
		return SSOConfigView{}, fmt.Errorf("save sso link: %w", err)
	}
	// Drop a stale challenge when the domain changed so we don't show a DNS record
	// for the wrong domain. The operator re-generates for the new domain.
	if priorDomain != emailDomain {
		if err := s.store.SetTenantSSODomainVerification(ctx, db.SetTenantSSODomainVerificationParams{TenantID: tenant.ID}); err != nil {
			return SSOConfigView{}, fmt.Errorf("reset domain challenge: %w", err)
		}
	}

	tenant.ExternalOrgID = pgtype.Text{String: externalOrgID, Valid: true}
	return s.ssoViewForTenant(ctx, tenant)
}

// SSOProvisionInput is the platform-admin request to onboard an SSO customer:
// create a Quill tenant, provision a matching Zitadel org (IdP + ORG_OWNER admin
// shells + external-only login), and pre-seed the admins so they land as Quill
// tenant admins on first SSO login.
type SSOProvisionInput struct {
	Slug         string
	Name         string
	Protocol     string // "oidc" | "saml"
	Issuer       string
	ClientID     string
	ClientSecret string
	EmailDomain  string
	AdminEmails  []string
}

// ProvisionSSOTenant creates a Quill tenant and its backing Zitadel org in one
// platform-admin action (see the flow in zitadel.ProvisionSSOOrg), links them, and
// records the admin emails as tenant-admin seeds. On any provisioning failure the
// freshly-created tenant is rolled back so no orphan remains.
func (s *Service) ProvisionSSOTenant(ctx context.Context, actor Actor, in SSOProvisionInput) (SSOConfigView, error) {
	if err := s.authorizePlatformAdmin(actor); err != nil {
		return SSOConfigView{}, err
	}
	if !s.orgProvisionerEnabled() {
		return SSOConfigView{}, fmt.Errorf("%w: SSO provisioning requires a configured identity provider (Zitadel management token)", ErrInvalidInput)
	}

	protocol := strings.TrimSpace(in.Protocol)
	if protocol == "" {
		protocol = "oidc"
	}
	if protocol != "oidc" && protocol != "saml" {
		return SSOConfigView{}, fmt.Errorf("%w: protocol must be 'oidc' or 'saml'", ErrInvalidInput)
	}
	issuer := strings.TrimSpace(in.Issuer)
	if issuer == "" {
		return SSOConfigView{}, fmt.Errorf("%w: an issuer / metadata URL is required", ErrInvalidInput)
	}
	clientID := strings.TrimSpace(in.ClientID)
	if protocol == "oidc" && clientID == "" {
		return SSOConfigView{}, fmt.Errorf("%w: a client id is required for OIDC", ErrInvalidInput)
	}
	emailDomain := strings.ToLower(strings.TrimSpace(in.EmailDomain))
	if emailDomain != "" && (strings.ContainsAny(emailDomain, " @/") || !strings.Contains(emailDomain, ".")) {
		return SSOConfigView{}, fmt.Errorf("%w: email domain must be a bare domain like acme.com", ErrInvalidInput)
	}

	// Validate the tenant slug and check availability BEFORE provisioning anything
	// in Zitadel, so we never create an org for a name that can't become a tenant.
	slug := normalizeSlug(in.Slug)
	name := strings.TrimSpace(in.Name)
	if !validSlug(slug) {
		return SSOConfigView{}, fmt.Errorf("%w: slug must be 1-63 chars of lowercase letters, digits, '-', '_' or '.', start alphanumeric, and not be a reserved word", ErrInvalidInput)
	}
	if name == "" {
		name = slug
	}
	if _, err := s.store.GetTenantBySlug(ctx, slug); err == nil {
		return SSOConfigView{}, ErrConflict
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return SSOConfigView{}, fmt.Errorf("lookup tenant: %w", err)
	}

	// Provision Zitadel FIRST. If it errors, tear down any partial org and return —
	// no Quill tenant is created. This is the atomicity the caller wants: a Zitadel
	// failure leaves nothing behind on either side.
	suffix, _ := randomToken(6)
	orgID, err := s.orgs.ProvisionSSOOrg(ctx, zitadel.SSOProvisionSpec{
		Protocol:     protocol,
		OrgName:      name + "-" + suffix,
		DisplayName:  name + " SSO",
		Issuer:       issuer,
		ClientID:     clientID,
		ClientSecret: in.ClientSecret,
		EmailDomain:  emailDomain,
		AdminEmails:  in.AdminEmails,
	})
	if err != nil {
		// ProvisionSSOOrg returns the created org id even on partial failure; delete
		// it so a retry with the same domain/name doesn't hit "already reserved".
		// Surface the real Zitadel reason rather than a generic 500.
		cctx, cancel := detachedContext(ctx)
		defer cancel()
		if orgID != "" {
			if delErr := s.orgs.DeleteSSOOrg(cctx, orgID); delErr != nil {
				s.logger.Error("failed to tear down zitadel org after sso provision failure", "org", orgID, "error", delErr)
			}
		}
		return SSOConfigView{}, fmt.Errorf("%w: SSO provisioning failed — %v", ErrInvalidInput, err)
	}

	// Pre-generate the domain-ownership challenge so the settings panel can show the
	// DNS record immediately, no extra click. Best-effort: if it fails (or there's
	// no domain), the operator can generate it later from the panel.
	var domainVal zitadel.DomainValidation
	if emailDomain != "" {
		if dv, gerr := s.orgs.GenerateDomainValidation(ctx, orgID, emailDomain); gerr != nil {
			s.logger.Warn("sso: could not pre-generate domain validation", "org", orgID, "error", gerr)
		} else {
			domainVal = dv
		}
	}

	// Zitadel is provisioned; commit the Quill side in one transaction (tenant +
	// link + sso row + challenge + admin seeds). If the transaction fails, tear the
	// Zitadel org back down so the two systems don't drift.
	var tenant db.Tenant
	txErr := s.store.InTx(ctx, func(q *db.Queries) error {
		t, err := q.CreateOrgTenant(ctx, db.CreateOrgTenantParams{Slug: slug, Name: name})
		if err != nil {
			return err
		}
		if err := q.SetTenantExternalOrg(ctx, db.SetTenantExternalOrgParams{
			ID:            t.ID,
			ExternalOrgID: pgtype.Text{String: orgID, Valid: true},
		}); err != nil {
			return err
		}
		if _, err := q.UpsertTenantSSO(ctx, db.UpsertTenantSSOParams{
			TenantID:    t.ID,
			Protocol:    protocol,
			EmailDomain: emailDomain,
			Enabled:     true,
		}); err != nil {
			return err
		}
		if domainVal.Token != "" {
			if err := q.SetTenantSSODomainVerification(ctx, db.SetTenantSSODomainVerificationParams{
				TenantID:                t.ID,
				DomainVerificationType:  domainVal.Type,
				DomainVerificationToken: domainVal.Token,
			}); err != nil {
				return err
			}
		}
		for _, email := range in.AdminEmails {
			e := strings.ToLower(strings.TrimSpace(email))
			if e == "" {
				continue
			}
			if err := q.AddTenantAdminSeed(ctx, db.AddTenantAdminSeedParams{TenantID: t.ID, Email: e}); err != nil {
				return err
			}
		}
		tenant = t
		return nil
	})
	if txErr != nil {
		cctx, cancel := detachedContext(ctx)
		defer cancel()
		if delErr := s.orgs.DeleteSSOOrg(cctx, orgID); delErr != nil {
			s.logger.Error("failed to tear down zitadel org after tenant commit failure", "org", orgID, "error", delErr)
		}
		if isUniqueViolation(txErr) {
			return SSOConfigView{}, ErrConflict
		}
		return SSOConfigView{}, fmt.Errorf("save sso tenant: %w", txErr)
	}

	tenant.ExternalOrgID = pgtype.Text{String: orgID, Valid: true}
	return s.ssoViewForTenant(ctx, tenant)
}

// DeleteOrgSSO unlinks an organization from its Zitadel org (platform admin only).
func (s *Service) DeleteOrgSSO(ctx context.Context, actor Actor, orgSlug string) error {
	if err := s.authorizePlatformAdmin(actor); err != nil {
		return err
	}
	tenant, err := s.getTenant(ctx, orgSlug)
	if err != nil {
		return err
	}
	if err := s.store.SetTenantExternalOrg(ctx, db.SetTenantExternalOrgParams{
		ID:            tenant.ID,
		ExternalOrgID: pgtype.Text{Valid: false},
	}); err != nil {
		return fmt.Errorf("unlink zitadel org: %w", err)
	}
	if err := s.store.DeleteTenantSSO(ctx, tenant.ID); err != nil {
		return fmt.Errorf("delete sso link: %w", err)
	}
	return nil
}

// GenerateSSODomainValidation (re)issues the DNS ownership challenge for an org's
// email domain (platform admin only) and persists it so the panel shows a stable
// record. Returns the refreshed view carrying the record to publish.
func (s *Service) GenerateSSODomainValidation(ctx context.Context, actor Actor, orgSlug string) (SSOConfigView, error) {
	tenant, domain, err := s.ssoDomainTarget(ctx, actor, orgSlug)
	if err != nil {
		return SSOConfigView{}, err
	}
	dv, err := s.orgs.GenerateDomainValidation(ctx, tenant.ExternalOrgID.String, domain)
	if err != nil {
		return SSOConfigView{}, fmt.Errorf("%w: could not generate a verification record — %v", ErrInvalidInput, err)
	}
	if err := s.store.SetTenantSSODomainVerification(ctx, db.SetTenantSSODomainVerificationParams{
		TenantID:                tenant.ID,
		DomainVerificationType:  dv.Type,
		DomainVerificationToken: dv.Token,
	}); err != nil {
		return SSOConfigView{}, fmt.Errorf("save domain challenge: %w", err)
	}
	return s.ssoViewForTenant(ctx, tenant)
}

// CheckSSODomainVerification asks Zitadel to validate the outstanding challenge
// (platform admin only), then returns the refreshed view. A challenge that isn't
// satisfied yet (e.g. the DNS record hasn't propagated) is a user-facing error,
// not a server failure.
func (s *Service) CheckSSODomainVerification(ctx context.Context, actor Actor, orgSlug string) (SSOConfigView, error) {
	tenant, domain, err := s.ssoDomainTarget(ctx, actor, orgSlug)
	if err != nil {
		return SSOConfigView{}, err
	}
	if err := s.orgs.ValidateDomain(ctx, tenant.ExternalOrgID.String, domain); err != nil {
		return SSOConfigView{}, fmt.Errorf("%w: the domain isn't verified yet — confirm the DNS TXT record is published and allow time for it to propagate", ErrInvalidInput)
	}
	return s.ssoViewForTenant(ctx, tenant)
}

// ssoDomainTarget resolves and authorizes an org for a domain-verification action:
// platform admin, an active org provisioner, a linked Zitadel org, and a
// configured email domain (the thing being verified).
func (s *Service) ssoDomainTarget(ctx context.Context, actor Actor, orgSlug string) (db.Tenant, string, error) {
	if err := s.authorizePlatformAdmin(actor); err != nil {
		return db.Tenant{}, "", err
	}
	if !s.orgProvisionerEnabled() {
		return db.Tenant{}, "", fmt.Errorf("%w: domain verification requires a configured identity provider (Zitadel management token)", ErrInvalidInput)
	}
	tenant, err := s.getTenant(ctx, orgSlug)
	if err != nil {
		return db.Tenant{}, "", err
	}
	if !tenant.ExternalOrgID.Valid || tenant.ExternalOrgID.String == "" {
		return db.Tenant{}, "", fmt.Errorf("%w: link a Zitadel organization before verifying a domain", ErrInvalidInput)
	}
	row, err := s.store.GetTenantSSO(ctx, tenant.ID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return db.Tenant{}, "", fmt.Errorf("%w: set an email domain before verifying it", ErrInvalidInput)
		}
		return db.Tenant{}, "", fmt.Errorf("load sso config: %w", err)
	}
	if row.EmailDomain == "" {
		return db.Tenant{}, "", fmt.Errorf("%w: set an email domain before verifying it", ErrInvalidInput)
	}
	return tenant, row.EmailDomain, nil
}

// ssoViewForTenant projects a tenant's stored SSO link into its API-facing form.
// It live-reads domain-verification status from Zitadel (best-effort: a failure
// leaves DomainStatusKnown false rather than erroring the whole view).
func (s *Service) ssoViewForTenant(ctx context.Context, tenant db.Tenant) (SSOConfigView, error) {
	linked := tenant.ExternalOrgID.Valid && tenant.ExternalOrgID.String != ""
	view := SSOConfigView{
		Configured:    linked,
		ExternalOrgID: tenant.ExternalOrgID.String,
	}
	row, err := s.store.GetTenantSSO(ctx, tenant.ID)
	if err == nil {
		view.EmailDomain = row.EmailDomain
		view.UpdatedAt = row.UpdatedAt
		if row.DomainVerificationToken != "" && row.EmailDomain != "" {
			view.DomainRecordType = row.DomainVerificationType
			view.DomainRecordName = "_zitadel-challenge." + row.EmailDomain
			view.DomainRecordValue = row.DomainVerificationToken
		}
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return SSOConfigView{}, fmt.Errorf("load sso config: %w", err)
	}

	// Live-read the verified flag from Zitadel — the source of truth. Best-effort:
	// if we can't reach it, leave DomainStatusKnown false.
	if linked && view.EmailDomain != "" && s.orgProvisionerEnabled() {
		if verified, err := s.orgs.IsDomainVerified(ctx, tenant.ExternalOrgID.String, view.EmailDomain); err != nil {
			s.logger.Warn("sso: could not read domain verification status", "tenant", tenant.ID, "error", err)
		} else {
			view.DomainVerified = verified
			view.DomainStatusKnown = true
		}
	}
	return view, nil
}
