package zitadel

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// SSO org provisioning against the Zitadel Management API.
//
// A platform admin onboards an SSO customer by provisioning a dedicated Zitadel
// org configured for external-IdP + auto-linking. The flow (see ProvisionSSOOrg)
// pre-creates credential-less user shells for the customer's admins so that, when
// they first authenticate through the IdP, Zitadel links the external identity to
// the shell (by verified email) rather than minting a fresh user — they land as
// ORG_OWNER logging in via the IdP, with no password and no init email.

// SSOProvisionSpec is a customer's SSO configuration in the form Zitadel needs.
// For SAML, Issuer carries the metadata URL and ClientID/ClientSecret are unused.
type SSOProvisionSpec struct {
	Protocol     string // "oidc" | "saml"
	OrgName      string // Zitadel org display name (internal)
	DisplayName  string // IdP name shown in Zitadel
	Issuer       string // OIDC issuer, or SAML metadata URL
	ClientID     string
	ClientSecret string
	EmailDomain  string   // added to the org for domain-discovery routing (still needs verification)
	AdminEmails  []string // pre-created as ORG_OWNER shells for IdP auto-linking
}

// ProvisionSSOOrg creates and configures a customer's Zitadel org, in order:
// (1) create org, (2) add the external IdP with auto-link-by-verified-email (+
// the email domain), (3) create silent credential-less user shells for each admin,
// (4) grant them ORG_OWNER, (5) disable local auth last and attach the IdP to the
// login policy. Returns the new org id. It is not idempotent at the org level —
// callers should provision once per customer.
func (c *Client) ProvisionSSOOrg(ctx context.Context, spec SSOProvisionSpec) (string, error) {
	if !c.Enabled() {
		return "", fmt.Errorf("zitadel management client not configured")
	}

	orgID, err := c.createOrg(ctx, spec.OrgName)
	if err != nil {
		return "", fmt.Errorf("create org: %w", err)
	}

	if spec.EmailDomain != "" {
		if err := c.addOrgDomain(ctx, orgID, spec.EmailDomain); err != nil {
			return orgID, fmt.Errorf("add org domain: %w", err)
		}
	}

	var idpID string
	switch strings.ToLower(spec.Protocol) {
	case "saml":
		idpID, err = c.addSAMLProvider(ctx, orgID, spec)
	default:
		idpID, err = c.addGenericOIDCProvider(ctx, orgID, spec)
	}
	if err != nil {
		return orgID, fmt.Errorf("add idp: %w", err)
	}

	// Pre-create the admin shells and grant ORG_OWNER while local auth is still on
	// (the default policy) so nothing blocks the setup.
	for _, email := range spec.AdminEmails {
		email = strings.TrimSpace(email)
		if email == "" {
			continue
		}
		userID, err := c.importShellUser(ctx, orgID, email)
		if err != nil {
			return orgID, fmt.Errorf("create admin shell %s: %w", email, err)
		}
		if err := c.addOrgOwner(ctx, orgID, userID); err != nil {
			return orgID, fmt.Errorf("grant ORG_OWNER to %s: %w", email, err)
		}
	}

	// Disable local auth last, then attach the IdP to the now-external-only policy.
	if err := c.setLoginPolicyExternalOnly(ctx, orgID); err != nil {
		return orgID, fmt.Errorf("set login policy: %w", err)
	}
	if err := c.addIDPToLoginPolicy(ctx, orgID, idpID); err != nil {
		return orgID, fmt.Errorf("attach idp to login policy: %w", err)
	}
	return orgID, nil
}

// DeleteSSOOrg removes a Zitadel org (scoped via the org-id header). Used to tear
// down a partially-provisioned org after a failure so a retry doesn't hit "domain
// already reserved" from the orphan. Tolerant of an already-gone org.
func (c *Client) DeleteSSOOrg(ctx context.Context, orgID string) error {
	if !c.Enabled() {
		return fmt.Errorf("zitadel management client not configured")
	}
	if orgID == "" {
		return nil
	}
	return c.doTolerant(ctx, http.MethodDelete, "/management/v1/orgs/me", orgID, nil)
}

// createOrg creates a Zitadel organization and returns its id.
func (c *Client) createOrg(ctx context.Context, name string) (string, error) {
	var out struct {
		ID string `json:"id"`
	}
	if err := c.do(ctx, http.MethodPost, "/management/v1/orgs", "", map[string]any{"name": name}, &out); err != nil {
		return "", err
	}
	if out.ID == "" {
		return "", fmt.Errorf("zitadel create org returned no id")
	}
	return out.ID, nil
}

// addOrgDomain registers the email domain on the org. It still requires ownership
// verification before Zitadel's domain discovery routes to it; that is surfaced to
// the operator rather than automated. NOT tolerant: on a fresh org any failure
// (e.g. the domain is reserved by another org) is real and must surface so the
// caller tears the org down instead of silently continuing without the domain.
func (c *Client) addOrgDomain(ctx context.Context, orgID, domain string) error {
	return c.do(ctx, http.MethodPost, "/management/v1/orgs/me/domains", orgID, map[string]any{
		"domain": domain,
	}, nil)
}

func (c *Client) addGenericOIDCProvider(ctx context.Context, orgID string, spec SSOProvisionSpec) (string, error) {
	body := map[string]any{
		"name":         spec.DisplayName,
		"issuer":       spec.Issuer,
		"clientId":     spec.ClientID,
		"clientSecret": spec.ClientSecret,
		"scopes":       []string{"openid", "profile", "email"},
		"providerOptions": map[string]any{
			"isLinkingAllowed":  true,
			"isCreationAllowed": true,
			"isAutoCreation":    true,
			"isAutoUpdate":      true,
			"autoLinking":       "AUTO_LINKING_OPTION_EMAIL",
		},
	}
	var out struct {
		ID string `json:"id"`
	}
	if err := c.do(ctx, http.MethodPost, "/management/v1/idps/generic_oidc", orgID, body, &out); err != nil {
		return "", err
	}
	if out.ID == "" {
		return "", fmt.Errorf("zitadel add oidc idp returned no id")
	}
	return out.ID, nil
}

func (c *Client) addSAMLProvider(ctx context.Context, orgID string, spec SSOProvisionSpec) (string, error) {
	body := map[string]any{
		"name":        spec.DisplayName,
		"metadataUrl": spec.Issuer,
		"binding":     "SAML_BINDING_POST",
		"providerOptions": map[string]any{
			"isLinkingAllowed":  true,
			"isCreationAllowed": true,
			"isAutoCreation":    true,
			"isAutoUpdate":      true,
			"autoLinking":       "AUTO_LINKING_OPTION_EMAIL",
		},
	}
	var out struct {
		ID string `json:"id"`
	}
	if err := c.do(ctx, http.MethodPost, "/management/v1/idps/saml", orgID, body, &out); err != nil {
		return "", err
	}
	if out.ID == "" {
		return "", fmt.Errorf("zitadel add saml idp returned no id")
	}
	return out.ID, nil
}

// importShellUser creates a credential-less user shell via the _import endpoint,
// which (unlike /users/human) sends NO notification email — exactly what we want
// for a user who will authenticate through the IdP. The verified email is what the
// IdP auto-links against on first login. Returns the created user id.
func (c *Client) importShellUser(ctx context.Context, orgID, email string) (string, error) {
	first, last := splitName("", email)
	body := map[string]any{
		"userName": email,
		"profile": map[string]any{
			"firstName":   first,
			"lastName":    last,
			"displayName": email,
		},
		"email": map[string]any{
			"email":           email,
			"isEmailVerified": true,
		},
	}
	var out struct {
		UserID string `json:"userId"`
	}
	if err := c.do(ctx, http.MethodPost, "/management/v1/users/human/_import", orgID, body, &out); err != nil {
		return "", err
	}
	if out.UserID == "" {
		return "", fmt.Errorf("zitadel import user returned no id")
	}
	return out.UserID, nil
}

// addOrgOwner grants a user the ORG_OWNER role in the org.
func (c *Client) addOrgOwner(ctx context.Context, orgID, userID string) error {
	return c.do(ctx, http.MethodPost, "/management/v1/orgs/me/members", orgID, map[string]any{
		"userId": userID,
		"roles":  []string{"ORG_OWNER"},
	}, nil)
}

// setLoginPolicyExternalOnly makes the org authenticate exclusively through
// external IdPs: password login off, external IdP on, and registration on so a
// first-time federated user is JIT-provisioned (or auto-linked) rather than
// looping on Zitadel's "external user not found" prompt.
func (c *Client) setLoginPolicyExternalOnly(ctx context.Context, orgID string) error {
	return c.putLoginPolicy(ctx, orgID, false, true)
}

func (c *Client) putLoginPolicy(ctx context.Context, orgID string, allowPassword, allowRegister bool) error {
	body := map[string]any{
		"allowUsernamePassword": allowPassword,
		"allowRegister":         allowRegister,
		"allowExternalIdp":      true,
		"forceMfa":              false,
		"passwordlessType":      "PASSWORDLESS_TYPE_NOT_ALLOWED",
	}
	// An org inherits the instance default login policy until a custom one is
	// created: POST creates it, and a 409 means one already exists, so update it
	// with PUT.
	status, snippet, err := c.doStatus(ctx, http.MethodPost, "/management/v1/policies/login", orgID, body, nil)
	if err != nil {
		return err
	}
	if status >= 200 && status < 300 {
		return nil
	}
	if status != http.StatusConflict {
		return &apiError{method: http.MethodPost, path: "/management/v1/policies/login", status: status, snippet: snippet}
	}
	return c.do(ctx, http.MethodPut, "/management/v1/policies/login", orgID, body, nil)
}

func (c *Client) addIDPToLoginPolicy(ctx context.Context, orgID, idpID string) error {
	return c.doTolerant(ctx, http.MethodPost, "/management/v1/policies/login/idps", orgID, map[string]any{
		"idpId":     idpID,
		"ownerType": "IDP_OWNER_TYPE_ORG",
	})
}

// Domain-ownership verification.
//
// Zitadel's domain discovery only routes a login to a customer's org when that
// org's email domain is *verified*. Verification is a challenge: Zitadel issues a
// token, the customer publishes it (a DNS TXT record), and Zitadel checks it.
// Quill drives all three steps from its own UI so onboarding never leaves Quill,
// but Zitadel remains the source of truth for the verified flag (live-read via
// IsDomainVerified) — Quill only persists the challenge itself.

// DomainValidation is a generated domain-ownership challenge to show the operator.
// For DNS, the customer adds a TXT record named RecordName with value RecordValue.
type DomainValidation struct {
	Type        string // "dns" | "http"
	Token       string // the Zitadel-issued challenge token
	RecordName  string // DNS: the TXT record name to create (e.g. _zitadel-challenge.acme.com)
	RecordValue string // DNS: the TXT record value (the token)
	URL         string // HTTP: the well-known URL to host the token at
}

// GenerateDomainValidation asks Zitadel to (re)issue a DNS ownership challenge for
// the org's domain and returns the record the customer must publish. Each call
// rotates the token, so callers persist the result rather than re-generating.
func (c *Client) GenerateDomainValidation(ctx context.Context, orgID, domain string) (DomainValidation, error) {
	if !c.Enabled() {
		return DomainValidation{}, fmt.Errorf("zitadel management client not configured")
	}
	// The domain must be registered on the org before a challenge can be generated
	// — _generate_validation 404s otherwise. Quill's provision flow adds it, but a
	// hand-linked org (SetOrgSSO) or a changed domain won't have it yet, so add it
	// here. A 409 means it already exists somewhere; any other non-2xx (e.g. a 404
	// = org id not found) is a real error and surfaces.
	addStatus, addSnippet, err := c.doStatus(ctx, http.MethodPost, "/management/v1/orgs/me/domains", orgID, map[string]any{
		"domain": domain,
	}, nil)
	if err != nil {
		return DomainValidation{}, err
	}
	if (addStatus < 200 || addStatus >= 300) && addStatus != http.StatusConflict {
		return DomainValidation{}, &apiError{method: http.MethodPost, path: "/management/v1/orgs/me/domains", status: addStatus, snippet: addSnippet}
	}

	// Confirm the domain is actually attached to THIS org before generating. The
	// _generate_validation 404 is opaque — Zitadel returns the same "Not Found" body
	// whether the domain isn't on the org or the org id is wrong — so we check via
	// the domain search and produce an actionable error listing what the org has.
	if domains, derr := c.listOrgDomains(ctx, orgID); derr == nil {
		found := false
		names := make([]string, 0, len(domains))
		for _, d := range domains {
			names = append(names, d.Domain)
			if strings.EqualFold(d.Domain, domain) {
				found = true
			}
		}
		if !found {
			have := "none"
			if len(names) > 0 {
				have = strings.Join(names, ", ")
			}
			return DomainValidation{}, fmt.Errorf("domain %q is not attached to Zitadel org %s (org domains: %s) — the linked org id may be wrong, or the domain is claimed by another org", domain, orgID, have)
		}
	}

	var out struct {
		Token string `json:"token"`
		URL   string `json:"url"`
	}
	path := "/management/v1/orgs/me/domains/" + url.PathEscape(domain) + "/_generate_validation"
	if err := c.do(ctx, http.MethodPost, path, orgID, map[string]any{
		"type": "ORG_DOMAIN_VALIDATION_TYPE_DNS",
	}, &out); err != nil {
		return DomainValidation{}, err
	}
	return DomainValidation{
		Type:        "dns",
		Token:       out.Token,
		RecordName:  "_zitadel-challenge." + domain,
		RecordValue: out.Token,
		URL:         out.URL,
	}, nil
}

// ValidateDomain asks Zitadel to check the previously generated challenge for the
// org's domain. A non-2xx (e.g. the DNS record isn't present yet) surfaces as an
// error so the caller can tell the operator verification hasn't completed.
func (c *Client) ValidateDomain(ctx context.Context, orgID, domain string) error {
	if !c.Enabled() {
		return fmt.Errorf("zitadel management client not configured")
	}
	path := "/management/v1/orgs/me/domains/" + url.PathEscape(domain) + "/_validation"
	return c.do(ctx, http.MethodPost, path, orgID, map[string]any{}, nil)
}

// IsDomainVerified live-reads whether the org's domain is verified in Zitadel (the
// source of truth). Matches the domain case-insensitively.
func (c *Client) IsDomainVerified(ctx context.Context, orgID, domain string) (bool, error) {
	if !c.Enabled() {
		return false, fmt.Errorf("zitadel management client not configured")
	}
	domains, err := c.listOrgDomains(ctx, orgID)
	if err != nil {
		return false, err
	}
	for _, d := range domains {
		if strings.EqualFold(d.Domain, domain) {
			return d.IsVerified, nil
		}
	}
	return false, nil
}

// orgDomain is one row of an org's domain list.
type orgDomain struct {
	Domain     string
	IsVerified bool
}

// listOrgDomains returns the domains registered on the org (scoped by orgID).
// Zitadel's ListOrgDomains result carries the name in `domainName`; older/other
// builds have used `domain`, so we read whichever is populated.
func (c *Client) listOrgDomains(ctx context.Context, orgID string) ([]orgDomain, error) {
	var out struct {
		Result []struct {
			Domain     string `json:"domain"`
			DomainName string `json:"domainName"`
			IsVerified bool   `json:"isVerified"`
		} `json:"result"`
	}
	if err := c.do(ctx, http.MethodPost, "/management/v1/orgs/me/domains/_search", orgID, map[string]any{}, &out); err != nil {
		return nil, err
	}
	domains := make([]orgDomain, 0, len(out.Result))
	for _, d := range out.Result {
		name := d.DomainName
		if name == "" {
			name = d.Domain
		}
		domains = append(domains, orgDomain{Domain: name, IsVerified: d.IsVerified})
	}
	return domains, nil
}
