package zitadel

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"
)

// Org-level SSO: a Quill organization with SSO enabled gets its own Zitadel
// org holding the customer's identity provider behind an SSO-only login
// policy; its verified email domain routes logins there through Zitadel's
// domain discovery. Request shapes were verified against our Zitadel
// (v2.65.1) on 2026-11-16 — note that this instance does NOT validate org
// domains, so callers must only add domains they have verified themselves.

// ErrConflict / ErrNotFound classify Zitadel's 409 / 404 answers so callers
// can treat "already there" and "already gone" as success.
var (
	ErrConflict = errors.New("zitadel: already exists")
	ErrNotFound = errors.New("zitadel: not found")
)

// Issuer is the Zitadel base URL (used to build IdP callback URLs).
func (c *Client) Issuer() string { return c.issuer }

// providerOptions lets people from the customer's IdP sign in without a
// prior Zitadel account (created on first login) and keeps profiles synced.
var providerOptions = map[string]bool{
	"isLinkingAllowed": true, "isCreationAllowed": true, "isAutoCreation": true, "isAutoUpdate": true,
}

// OIDCProvider is a generic OIDC identity provider.
type OIDCProvider struct {
	Name         string
	Issuer       string
	ClientID     string
	ClientSecret string // required on create; empty on update keeps the stored one
}

// UpsertOIDCProvider creates (idpID == "") or updates a generic OIDC provider
// in the org and returns its id.
func (c *Client) UpsertOIDCProvider(ctx context.Context, orgID, idpID string, p OIDCProvider) (string, error) {
	body := map[string]any{
		"name": p.Name, "issuer": p.Issuer, "clientId": p.ClientID,
		"scopes": []string{"openid", "profile", "email"}, "isIdTokenMapping": true, "providerOptions": providerOptions,
	}
	if p.ClientSecret != "" {
		body["clientSecret"] = p.ClientSecret
	}
	if idpID != "" {
		return idpID, c.do(ctx, http.MethodPut, "/management/v1/idps/generic_oidc/"+url.PathEscape(idpID), orgID, body, nil)
	}
	var out struct {
		ID string `json:"id"`
	}
	if err := c.do(ctx, http.MethodPost, "/management/v1/idps/generic_oidc", orgID, body, &out); err != nil {
		return "", err
	}
	return out.ID, nil
}

// UpsertSAMLProvider creates (idpID == "") or updates a SAML provider from
// the IdP's metadata XML and returns its id.
func (c *Client) UpsertSAMLProvider(ctx context.Context, orgID, idpID, name string, metadataXML []byte) (string, error) {
	body := map[string]any{
		"name": name, "metadataXml": base64.StdEncoding.EncodeToString(metadataXML),
		"binding": "SAML_BINDING_POST", "withSignedRequest": true, "providerOptions": providerOptions,
	}
	if idpID != "" {
		return idpID, c.do(ctx, http.MethodPut, "/management/v1/idps/saml/"+url.PathEscape(idpID), orgID, body, nil)
	}
	var out struct {
		ID string `json:"id"`
	}
	if err := c.do(ctx, http.MethodPost, "/management/v1/idps/saml", orgID, body, &out); err != nil {
		return "", err
	}
	return out.ID, nil
}

// DeleteProvider removes an identity provider (and its login-policy entry).
func (c *Client) DeleteProvider(ctx context.Context, orgID, idpID string) error {
	err := c.do(ctx, http.MethodDelete, "/management/v1/policies/login/idps/"+url.PathEscape(idpID), orgID, nil, nil)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return err
	}
	err = c.do(ctx, http.MethodDelete, "/management/v1/idps/templates/"+url.PathEscape(idpID), orgID, nil, nil)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	return err
}

// EnsureSSOLoginPolicy gives the org a login policy that only offers its
// identity provider (no passwords, no self-registration) so domain discovery
// sends people straight to their company login.
func (c *Client) EnsureSSOLoginPolicy(ctx context.Context, orgID, idpID string) error {
	policy := map[string]any{
		"allowUsernamePassword": false, "allowRegister": false, "allowExternalIdp": true,
		"forceMfa": false, "passwordlessType": "PASSWORDLESS_TYPE_NOT_ALLOWED", "hidePasswordReset": true,
		"ignoreUnknownUsernames": false, "allowDomainDiscovery": true, "disableLoginWithEmail": false,
		"disableLoginWithPhone": true, "passwordCheckLifetime": "864000s", "externalLoginCheckLifetime": "864000s",
		"mfaInitSkipLifetime": "0s", "secondFactorCheckLifetime": "64800s", "multiFactorCheckLifetime": "43200s",
		"idps": []map[string]string{{"idpId": idpID, "ownerType": "IDP_OWNER_TYPE_ORG"}},
	}
	err := c.do(ctx, http.MethodPost, "/management/v1/policies/login", orgID, policy, nil)
	if err == nil {
		return nil
	}
	if !errors.Is(err, ErrConflict) {
		return err
	}
	// The org already has a custom policy: make sure this IdP is on it.
	err = c.do(ctx, http.MethodPost, "/management/v1/policies/login/idps", orgID,
		map[string]string{"idpId": idpID, "ownerType": "IDP_OWNER_TYPE_ORG"}, nil)
	if errors.Is(err, ErrConflict) {
		return nil
	}
	return err
}

// AddOrgDomain registers a (Quill-verified) domain on the org, which enables
// domain discovery for it. Already present is success.
func (c *Client) AddOrgDomain(ctx context.Context, orgID, domain string) error {
	err := c.do(ctx, http.MethodPost, "/management/v1/orgs/me/domains", orgID, map[string]string{"domain": domain}, nil)
	if errors.Is(err, ErrConflict) {
		return nil
	}
	return err
}

// RemoveOrgDomain stops routing a domain to the org. Already gone is success.
func (c *Client) RemoveOrgDomain(ctx context.Context, orgID, domain string) error {
	err := c.do(ctx, http.MethodDelete, "/management/v1/orgs/me/domains/"+url.PathEscape(domain), orgID, nil, nil)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	return err
}

// DeleteOrg removes a Zitadel org and everything in it (users, IdPs). Used
// for test-org teardown only.
func (c *Client) DeleteOrg(ctx context.Context, orgID string) error {
	err := c.do(ctx, http.MethodDelete, "/management/v1/orgs/me", orgID, nil, nil)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	return err
}

// classify maps Zitadel status codes onto the sentinel errors.
func classify(status int, err error) error {
	switch status {
	case http.StatusConflict:
		return fmt.Errorf("%w: %v", ErrConflict, err)
	case http.StatusNotFound:
		return fmt.Errorf("%w: %v", ErrNotFound, err)
	}
	return err
}

// EnsureOrgScopedUsernames gives the org a domain policy whose usernames are
// scoped to the org (userLoginMustBeDomain). Without it usernames are unique
// across the whole instance, and an SSO sign-in whose username (usually the
// email) matches any existing account can't be created automatically —
// Zitadel stops on "User already exists" (V3-DKcYh). Admin API; idempotent.
func (c *Client) EnsureOrgScopedUsernames(ctx context.Context, orgID string) error {
	err := c.do(ctx, http.MethodPost, "/admin/v1/orgs/"+url.PathEscape(orgID)+"/policies/domain", "", map[string]bool{
		"userLoginMustBeDomain": true, "validateOrgDomains": false, "smtpSenderAddressMatchesInstanceDomain": false,
	}, nil)
	if errors.Is(err, ErrConflict) {
		return nil
	}
	return err
}
