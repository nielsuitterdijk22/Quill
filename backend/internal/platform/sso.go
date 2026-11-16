package platform

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/nielsuitterdijk22/quill/internal/store"
	"github.com/nielsuitterdijk22/quill/internal/store/db"
	"github.com/nielsuitterdijk22/quill/internal/zitadel"
)

// Organization single sign-on. An org admin configures the company identity
// provider (OIDC or SAML) and the company email domain. Quill applies it to
// Zitadel: a dedicated Zitadel org holds the IdP behind an SSO-only login
// policy, and — once Quill has verified the domain through a DNS TXT record —
// the domain is registered on that org so Zitadel's domain discovery sends
// "@company.com" sign-ins straight to the company login. People who arrive
// through it join the Quill org automatically (auth.joinSSOOrg).
//
// Domain verification is Quill's job: our Zitadel instance does not validate
// org domains, so registering an unverified domain would let any org capture
// another company's (or gmail.com's) logins. Public mailbox providers are
// refused outright, and a verified domain belongs to one org only.
//
// The client secret is encrypted at rest (secretbox) and write-only.

// ssoMaxField bounds the free-text SSO fields.
const ssoMaxField = 500

// ssoVerifyPrefix is the TXT record name prefix and value prefix.
const (
	ssoVerifyRecordPrefix = "_quill-verification."
	ssoVerifyValuePrefix  = "quill-verification="
)

// SSOBackend applies SSO to the identity provider (satisfied by
// internal/zitadel.Client).
type SSOBackend interface {
	Enabled() bool
	Issuer() string
	CreateOrg(ctx context.Context, name string) (string, error)
	EnsureOrgScopedUsernames(ctx context.Context, orgID string) error
	UpsertOIDCProvider(ctx context.Context, orgID, idpID string, p zitadel.OIDCProvider) (string, error)
	UpsertSAMLProvider(ctx context.Context, orgID, idpID, name string, metadataXML []byte) (string, error)
	DeleteProvider(ctx context.Context, orgID, idpID string) error
	EnsureSSOLoginPolicy(ctx context.Context, orgID, idpID string) error
	AddOrgDomain(ctx context.Context, orgID, domain string) error
	RemoveOrgDomain(ctx context.Context, orgID, domain string) error
	DeleteOrg(ctx context.Context, orgID string) error
}

// WithSSO installs the SSO backend.
func (s *Service) WithSSO(b SSOBackend) *Service {
	s.sso = b
	return s
}

func (s *Service) ssoEnabled() bool { return s.sso != nil && s.sso.Enabled() }

// publicMailDomains can never be claimed by an organization.
var publicMailDomains = map[string]bool{
	"gmail.com": true, "googlemail.com": true, "outlook.com": true, "hotmail.com": true, "live.com": true,
	"msn.com": true, "yahoo.com": true, "ymail.com": true, "icloud.com": true, "me.com": true, "mac.com": true,
	"aol.com": true, "proton.me": true, "protonmail.com": true, "pm.me": true, "gmx.com": true, "gmx.net": true,
	"gmx.de": true, "web.de": true, "mail.com": true, "yandex.com": true, "yandex.ru": true, "zoho.com": true,
	"tutanota.com": true, "tuta.io": true, "fastmail.com": true, "hey.com": true, "mozmail.com": true,
	"duck.com": true, "ziggo.nl": true, "kpnmail.nl": true, "hetnet.nl": true, "planet.nl": true, "xs4all.nl": true,
	"live.nl": true, "hotmail.nl": true, "outlook.de": true, "t-online.de": true, "orange.fr": true, "free.fr": true,
	"laposte.net": true, "libero.it": true, "black-tulip.nl": true,
}

// SSOConfigInput is the desired SSO configuration for an organization.
type SSOConfigInput struct {
	Protocol string
	// Issuer is the OIDC issuer URL, or the SAML metadata URL.
	Issuer   string
	ClientID string
	// ClientSecret, when non-empty, replaces the stored secret; empty preserves
	// the existing one (write-only semantics).
	ClientSecret string
	EmailDomain  string
	Enabled      bool
}

// SSOConfigView is an organization's SSO configuration in API-facing form. It
// never carries the client secret — only whether one is set.
type SSOConfigView struct {
	Configured  bool
	Protocol    string
	Issuer      string
	ClientID    string
	EmailDomain string
	Enabled     bool
	HasSecret   bool
	UpdatedAt   time.Time

	// DomainVerified: the TXT record was found; VerifyRecordName/Value is
	// the record to publish while it isn't.
	DomainVerified    bool
	VerifyRecordName  string
	VerifyRecordValue string
	// Active: the IdP is live in Zitadel; Routing: the domain sends
	// sign-ins to it.
	Active  bool
	Routing bool
	// What to register at the company IdP.
	RedirectURI   string // OIDC
	SAMLEntityID  string // SAML (also the SP metadata URL)
	SAMLACSURL    string // SAML
	ZitadelOrgID  string // for org-scoped sign-in links
	LastError     string
	Available     bool // SSO login is wired on this server
	PlanAllowsSSO bool
}

// GetOrgSSO returns an organization's SSO configuration (admin only). When none
// is configured it returns a zero view with Configured=false.
func (s *Service) GetOrgSSO(ctx context.Context, actor Actor, orgSlug string) (SSOConfigView, error) {
	tenant, err := s.getTenant(ctx, orgSlug)
	if err != nil {
		return SSOConfigView{}, err
	}
	if err := s.authorizeTenantAdmin(ctx, actor, tenant.ID); err != nil {
		return SSOConfigView{}, err
	}
	row, err := s.store.GetTenantSSO(ctx, tenant.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		return SSOConfigView{Configured: false, Protocol: "oidc", Available: s.ssoEnabled(), PlanAllowsSSO: scimAllowedPlan(tenant.Plan)}, nil
	}
	if err != nil {
		return SSOConfigView{}, fmt.Errorf("load sso config: %w", err)
	}
	st, err := s.store.GetSSOState(ctx, tenant.ID)
	if err != nil {
		return SSOConfigView{}, fmt.Errorf("load sso state: %w", err)
	}
	return s.ssoView(tenant, row, st), nil
}

// SetOrgSSO creates or updates an organization's SSO configuration (admin
// only) and applies it to the identity provider. A blank ClientSecret
// preserves the stored one. The config is saved even when applying fails; the
// view's LastError then says why, and saving again retries.
func (s *Service) SetOrgSSO(ctx context.Context, actor Actor, orgSlug string, in SSOConfigInput) (SSOConfigView, error) {
	tenant, err := s.getTenant(ctx, orgSlug)
	if err != nil {
		return SSOConfigView{}, err
	}
	if err := s.authorizeTenantAdmin(ctx, actor, tenant.ID); err != nil {
		return SSOConfigView{}, err
	}

	protocol := strings.TrimSpace(in.Protocol)
	if protocol == "" {
		protocol = "oidc"
	}
	if protocol != "oidc" && protocol != "saml" {
		return SSOConfigView{}, fmt.Errorf("%w: protocol must be 'oidc' or 'saml'", ErrInvalidInput)
	}
	issuer := strings.TrimRight(strings.TrimSpace(in.Issuer), "/")
	if protocol == "saml" {
		issuer = strings.TrimSpace(in.Issuer) // metadata URLs are taken verbatim
	}
	clientID := strings.TrimSpace(in.ClientID)
	emailDomain := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(in.EmailDomain)), ".")
	for _, f := range []string{issuer, clientID, emailDomain} {
		if len(f) > ssoMaxField {
			return SSOConfigView{}, fmt.Errorf("%w: an SSO field is too long", ErrInvalidInput)
		}
	}
	if emailDomain != "" && (strings.ContainsAny(emailDomain, " @/") || !strings.Contains(emailDomain, ".")) {
		return SSOConfigView{}, fmt.Errorf("%w: email domain must be a bare domain like acme.com", ErrInvalidInput)
	}
	if publicMailDomains[emailDomain] {
		return SSOConfigView{}, fmt.Errorf("%w: %s is a public email provider and can't be claimed by an organization", ErrInvalidInput, emailDomain)
	}
	if issuer != "" {
		u, err := url.Parse(issuer)
		if err != nil || u.Scheme != "https" || u.Host == "" {
			return SSOConfigView{}, fmt.Errorf("%w: the issuer / metadata URL must be an https URL", ErrInvalidInput)
		}
	}
	// Enabling requires enough to actually authenticate against, and a plan
	// that includes SSO.
	if in.Enabled {
		if issuer == "" {
			return SSOConfigView{}, fmt.Errorf("%w: an issuer / metadata URL is required to enable SSO", ErrInvalidInput)
		}
		if protocol == "oidc" && clientID == "" {
			return SSOConfigView{}, fmt.Errorf("%w: a client id is required to enable OIDC SSO", ErrInvalidInput)
		}
		if !scimAllowedPlan(tenant.Plan) {
			return SSOConfigView{}, fmt.Errorf("%w: single sign-on is part of the Team plan", ErrLimitReached)
		}
		if err := s.enforceTenantActive(ctx, tenant); err != nil {
			return SSOConfigView{}, err
		}
	}

	// Resolve the client secret: encrypt a newly-provided one, else keep the
	// existing ciphertext (write-only).
	var ciphertext, nonce []byte
	existing, existsErr := s.store.GetTenantSSO(ctx, tenant.ID)
	if existsErr != nil && !errors.Is(existsErr, pgx.ErrNoRows) {
		return SSOConfigView{}, fmt.Errorf("load sso config: %w", existsErr)
	}
	if strings.TrimSpace(in.ClientSecret) != "" {
		if len(in.ClientSecret) > ssoMaxField {
			return SSOConfigView{}, fmt.Errorf("%w: client secret is too long", ErrInvalidInput)
		}
		ct, n, err := s.cipher.Seal([]byte(in.ClientSecret))
		if err != nil {
			return SSOConfigView{}, fmt.Errorf("encrypt client secret: %w", err)
		}
		ciphertext, nonce = ct, n
	} else if existsErr == nil {
		ciphertext, nonce = existing.ClientSecretCiphertext, existing.ClientSecretNonce
	}
	if in.Enabled && protocol == "oidc" && len(ciphertext) == 0 {
		return SSOConfigView{}, fmt.Errorf("%w: a client secret is required to enable OIDC SSO", ErrInvalidInput)
	}

	row, err := s.store.UpsertTenantSSO(ctx, db.UpsertTenantSSOParams{
		TenantID:               tenant.ID,
		Protocol:               protocol,
		Issuer:                 issuer,
		ClientID:               clientID,
		ClientSecretCiphertext: ciphertext,
		ClientSecretNonce:      nonce,
		EmailDomain:            emailDomain,
		Enabled:                in.Enabled,
	})
	if err != nil {
		return SSOConfigView{}, fmt.Errorf("save sso config: %w", err)
	}

	st, err := s.store.GetSSOState(ctx, tenant.ID)
	if err != nil {
		return SSOConfigView{}, fmt.Errorf("load sso state: %w", err)
	}
	// A new (or removed) domain needs a fresh verification.
	if existsErr != nil || !strings.EqualFold(existing.EmailDomain, emailDomain) || (emailDomain != "" && st.DomainVerifyToken == "") {
		st.DomainVerifiedAt = nil
		st.DomainVerifyToken = ""
		if emailDomain != "" {
			tok, err := randomToken(18)
			if err != nil {
				return SSOConfigView{}, err
			}
			st.DomainVerifyToken = strings.ToLower(strings.NewReplacer("-", "", "_", "").Replace(tok))
		}
	}
	st = s.applySSO(ctx, tenant, row, st)
	if err := s.store.SetSSOState(ctx, tenant.ID, st); err != nil {
		return SSOConfigView{}, fmt.Errorf("save sso state: %w", err)
	}
	return s.ssoView(tenant, row, st), nil
}

// VerifyOrgSSODomain checks the domain's TXT record and, when it matches,
// marks the domain verified and (if SSO is enabled) starts routing it.
func (s *Service) VerifyOrgSSODomain(ctx context.Context, actor Actor, orgSlug string) (SSOConfigView, error) {
	tenant, err := s.getTenant(ctx, orgSlug)
	if err != nil {
		return SSOConfigView{}, err
	}
	if err := s.authorizeTenantAdmin(ctx, actor, tenant.ID); err != nil {
		return SSOConfigView{}, err
	}
	row, err := s.store.GetTenantSSO(ctx, tenant.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		return SSOConfigView{}, ErrNotFound
	}
	if err != nil {
		return SSOConfigView{}, fmt.Errorf("load sso config: %w", err)
	}
	st, err := s.store.GetSSOState(ctx, tenant.ID)
	if err != nil {
		return SSOConfigView{}, fmt.Errorf("load sso state: %w", err)
	}
	if row.EmailDomain == "" || st.DomainVerifyToken == "" {
		return SSOConfigView{}, fmt.Errorf("%w: set an email domain first", ErrInvalidInput)
	}
	if st.DomainVerifiedAt == nil {
		taken, err := s.store.DomainVerifiedByOtherTenant(ctx, tenant.ID, row.EmailDomain)
		if err != nil {
			return SSOConfigView{}, err
		}
		if taken {
			return SSOConfigView{}, fmt.Errorf("%w: %s is already verified by another organization", ErrConflict, row.EmailDomain)
		}
		want := ssoVerifyValuePrefix + st.DomainVerifyToken
		records, err := s.lookupTXT(ctx, ssoVerifyRecordPrefix+row.EmailDomain)
		found := false
		for _, r := range records {
			if strings.TrimSpace(r) == want {
				found = true
			}
		}
		if !found {
			detail := "no matching record yet"
			if err != nil {
				detail = "lookup failed: " + err.Error()
			}
			return SSOConfigView{}, fmt.Errorf("%w: TXT record %s%s = %q not found (%s); DNS changes can take a few minutes", ErrInvalidInput, ssoVerifyRecordPrefix, row.EmailDomain, want, detail)
		}
		now := s.now()
		st.DomainVerifiedAt = &now
	}
	st = s.applySSO(ctx, tenant, row, st)
	if err := s.store.SetSSOState(ctx, tenant.ID, st); err != nil {
		if isUniqueViolation(err) {
			return SSOConfigView{}, fmt.Errorf("%w: %s is already verified by another organization", ErrConflict, row.EmailDomain)
		}
		return SSOConfigView{}, fmt.Errorf("save sso state: %w", err)
	}
	return s.ssoView(tenant, row, st), nil
}

// DeleteOrgSSO removes an organization's SSO: stops routing its domain,
// removes the identity provider and forgets the configuration. The Zitadel
// org and the accounts in it stay (people keep their Quill accounts).
func (s *Service) DeleteOrgSSO(ctx context.Context, actor Actor, orgSlug string) error {
	tenant, err := s.getTenant(ctx, orgSlug)
	if err != nil {
		return err
	}
	if err := s.authorizeTenantAdmin(ctx, actor, tenant.ID); err != nil {
		return err
	}
	st, err := s.store.GetSSOState(ctx, tenant.ID)
	if err != nil {
		return fmt.Errorf("load sso state: %w", err)
	}
	if s.ssoEnabled() && st.ZitadelOrgID != "" {
		if st.RoutedDomain != "" {
			if err := s.sso.RemoveOrgDomain(ctx, st.ZitadelOrgID, st.RoutedDomain); err != nil {
				return fmt.Errorf("stop routing %s: %w", st.RoutedDomain, err)
			}
		}
		if st.ZitadelIdPID != "" {
			if err := s.sso.DeleteProvider(ctx, st.ZitadelOrgID, st.ZitadelIdPID); err != nil {
				return fmt.Errorf("remove identity provider: %w", err)
			}
		}
	}
	if st.ZitadelOrgID == "" {
		if err := s.store.DeleteTenantSSO(ctx, tenant.ID); err != nil {
			return fmt.Errorf("delete sso config: %w", err)
		}
		return nil
	}
	// Keep the link to the identity org: the accounts in it belong to this
	// organization, and setting SSO up again must reuse it rather than
	// orphan them in a second org.
	if _, err := s.store.UpsertTenantSSO(ctx, db.UpsertTenantSSOParams{TenantID: tenant.ID, Protocol: "oidc"}); err != nil {
		return fmt.Errorf("clear sso config: %w", err)
	}
	if err := s.store.SetSSOState(ctx, tenant.ID, store.SSOState{ZitadelOrgID: st.ZitadelOrgID}); err != nil {
		return fmt.Errorf("clear sso state: %w", err)
	}
	return nil
}

// applySSO converges the identity provider on the stored config and returns
// the resulting state (LastError set when a step failed; later steps are
// skipped so the state never claims more than what is live).
func (s *Service) applySSO(ctx context.Context, tenant db.Tenant, row db.TenantSsoConfig, st store.SSOState) store.SSOState {
	st.LastError = ""
	fail := func(step string, err error) store.SSOState {
		s.logger.Warn("sso apply failed", "tenant", tenant.Slug, "step", step, "error", err)
		st.LastError = step + ": " + err.Error()
		return st
	}
	if !s.ssoEnabled() {
		if row.Enabled {
			st.LastError = "single sign-on login is not available on this server yet"
		}
		return st
	}
	want := ""
	if row.Enabled && st.DomainVerifiedAt != nil {
		want = row.EmailDomain
	}
	// Stop routing a domain that is no longer wanted first (disable, domain
	// change, lost verification) — even if later steps fail.
	if st.RoutedDomain != "" && st.RoutedDomain != want && st.ZitadelOrgID != "" {
		if err := s.sso.RemoveOrgDomain(ctx, st.ZitadelOrgID, st.RoutedDomain); err != nil {
			return fail("stop routing "+st.RoutedDomain, err)
		}
		st.RoutedDomain = ""
	}
	if !row.Enabled {
		return st
	}
	if st.ZitadelOrgID == "" {
		id, err := s.sso.CreateOrg(ctx, "Quill SSO · "+tenant.Slug)
		if err != nil {
			return fail("create identity org", err)
		}
		st.ZitadelOrgID = id
		// Persist at once so a failure below never creates a second org.
		_ = s.store.SetSSOState(ctx, tenant.ID, st)
	}
	if err := s.sso.EnsureOrgScopedUsernames(ctx, st.ZitadelOrgID); err != nil {
		return fail("scope usernames to the identity org", err)
	}
	if st.ZitadelIdPID != "" && st.ZitadelIdPProtocol != row.Protocol {
		if err := s.sso.DeleteProvider(ctx, st.ZitadelOrgID, st.ZitadelIdPID); err != nil {
			return fail("replace identity provider", err)
		}
		st.ZitadelIdPID, st.ZitadelIdPProtocol = "", ""
	}
	name := strings.TrimSpace(tenant.Name)
	if name == "" {
		name = tenant.Slug
	}
	name += " SSO"
	switch row.Protocol {
	case "oidc":
		secret := ""
		if len(row.ClientSecretCiphertext) > 0 {
			plain, err := s.cipher.Open(row.ClientSecretCiphertext, row.ClientSecretNonce)
			if err != nil {
				return fail("decrypt client secret", err)
			}
			secret = string(plain)
		}
		id, err := s.sso.UpsertOIDCProvider(ctx, st.ZitadelOrgID, st.ZitadelIdPID, zitadel.OIDCProvider{
			Name: name, Issuer: row.Issuer, ClientID: row.ClientID, ClientSecret: secret,
		})
		if err != nil {
			return fail("configure OIDC provider", err)
		}
		st.ZitadelIdPID = id
	case "saml":
		xml, err := s.fetchSAMLMetadata(ctx, row.Issuer)
		if err != nil {
			return fail("fetch SAML metadata", err)
		}
		id, err := s.sso.UpsertSAMLProvider(ctx, st.ZitadelOrgID, st.ZitadelIdPID, name, xml)
		if err != nil {
			return fail("configure SAML provider", err)
		}
		st.ZitadelIdPID = id
	}
	st.ZitadelIdPProtocol = row.Protocol
	if err := s.sso.EnsureSSOLoginPolicy(ctx, st.ZitadelOrgID, st.ZitadelIdPID); err != nil {
		return fail("set login policy", err)
	}
	if want != "" && st.RoutedDomain != want {
		if err := s.sso.AddOrgDomain(ctx, st.ZitadelOrgID, want); err != nil {
			return fail("route "+want, err)
		}
		st.RoutedDomain = want
	}
	return st
}

// fetchSAMLMetadata downloads the IdP's metadata XML (https, ≤1 MiB, 10 s).
func (s *Service) fetchSAMLMetadata(ctx context.Context, metadataURL string) ([]byte, error) {
	if s.samlFetch != nil {
		return s.samlFetch(ctx, metadataURL)
	}
	u, err := url.Parse(metadataURL)
	if err != nil || u.Scheme != "https" {
		return nil, fmt.Errorf("metadata URL must be https")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, metadataURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("metadata URL answered %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if !strings.Contains(string(body), "EntityDescriptor") {
		return nil, fmt.Errorf("the URL did not return SAML metadata")
	}
	return body, nil
}

// lookupTXT resolves TXT records (overridable in tests).
func (s *Service) lookupTXT(ctx context.Context, name string) ([]string, error) {
	if s.txtLookup != nil {
		return s.txtLookup(ctx, name)
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return net.DefaultResolver.LookupTXT(ctx, name)
}

// ssoView projects the stored config + state into the API view, never
// exposing the encrypted secret.
func (s *Service) ssoView(tenant db.Tenant, row db.TenantSsoConfig, st store.SSOState) SSOConfigView {
	v := SSOConfigView{
		// A row that only remembers the identity org (SSO removed) is not
		// a configuration.
		Configured:     row.Issuer != "" || row.ClientID != "" || row.EmailDomain != "" || row.Enabled,
		Protocol:       row.Protocol,
		Issuer:         row.Issuer,
		ClientID:       row.ClientID,
		EmailDomain:    row.EmailDomain,
		Enabled:        row.Enabled,
		HasSecret:      len(row.ClientSecretCiphertext) > 0,
		UpdatedAt:      row.UpdatedAt,
		DomainVerified: st.DomainVerifiedAt != nil,
		Active:         row.Enabled && st.ZitadelIdPID != "" && st.LastError == "",
		Routing:        st.RoutedDomain != "",
		ZitadelOrgID:   st.ZitadelOrgID,
		LastError:      st.LastError,
		Available:      s.ssoEnabled(),
		PlanAllowsSSO:  scimAllowedPlan(tenant.Plan),
	}
	if row.EmailDomain != "" && st.DomainVerifyToken != "" {
		v.VerifyRecordName = ssoVerifyRecordPrefix + row.EmailDomain
		v.VerifyRecordValue = ssoVerifyValuePrefix + st.DomainVerifyToken
	}
	if s.ssoEnabled() {
		iss := strings.TrimRight(s.sso.Issuer(), "/")
		v.RedirectURI = iss + "/ui/login/login/externalidp/callback"
		if st.ZitadelIdPID != "" && row.Protocol == "saml" {
			v.SAMLEntityID = iss + "/idps/" + st.ZitadelIdPID + "/saml/metadata"
			v.SAMLACSURL = iss + "/idps/" + st.ZitadelIdPID + "/saml/acs"
		}
	}
	return v
}

// teardownSSOOrgs deletes the Zitadel orgs behind the given tenants' SSO
// (test-tenant purge only; real orgs keep their accounts).
func (s *Service) teardownSSOOrgs(ctx context.Context, tenantIDs []uuid.UUID) {
	if !s.ssoEnabled() || len(tenantIDs) == 0 {
		return
	}
	orgs, err := s.store.ListSSOZitadelOrgs(ctx, tenantIDs)
	if err != nil {
		s.logger.Warn("list sso orgs for teardown", "error", err)
		return
	}
	for _, o := range orgs {
		if err := s.sso.DeleteOrg(ctx, o); err != nil {
			s.logger.Warn("delete sso org", "org", o, "error", err)
		}
	}
}
