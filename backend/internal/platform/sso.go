package platform

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/nielsuitterdijk22/quill/internal/store/db"
)

// Organization SSO configuration. An org admin arranges how members of the org
// authenticate: the OIDC/SAML endpoint, client id, an encrypted client secret,
// and the email domain that routes users to this org. The client secret is
// encrypted at rest with the same secretbox cipher as pipeline secrets and is
// write-only over the API (reads only report whether one is set).
//
// This is the configuration + storage surface. Selecting this config at login
// time (email_domain -> org -> IdP) is an auth-layer follow-up; nothing here
// changes the login path yet.

// ssoMaxField bounds the free-text SSO fields.
const ssoMaxField = 500

// SSOConfigInput is the desired SSO configuration for an organization.
type SSOConfigInput struct {
	Protocol string
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
		return SSOConfigView{Configured: false, Protocol: "oidc"}, nil
	}
	if err != nil {
		return SSOConfigView{}, fmt.Errorf("load sso config: %w", err)
	}
	return ssoView(row), nil
}

// SetOrgSSO creates or updates an organization's SSO configuration (admin only).
// A blank ClientSecret preserves the currently-stored one.
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
	issuer := strings.TrimSpace(in.Issuer)
	clientID := strings.TrimSpace(in.ClientID)
	emailDomain := strings.ToLower(strings.TrimSpace(in.EmailDomain))
	for _, f := range []string{issuer, clientID, emailDomain} {
		if len(f) > ssoMaxField {
			return SSOConfigView{}, fmt.Errorf("%w: an SSO field is too long", ErrInvalidInput)
		}
	}
	if emailDomain != "" && (strings.ContainsAny(emailDomain, " @/") || !strings.Contains(emailDomain, ".")) {
		return SSOConfigView{}, fmt.Errorf("%w: email domain must be a bare domain like acme.com", ErrInvalidInput)
	}
	// Enabling requires enough to actually authenticate against.
	if in.Enabled {
		if issuer == "" {
			return SSOConfigView{}, fmt.Errorf("%w: an issuer / metadata URL is required to enable SSO", ErrInvalidInput)
		}
		if protocol == "oidc" && clientID == "" {
			return SSOConfigView{}, fmt.Errorf("%w: a client id is required to enable OIDC SSO", ErrInvalidInput)
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
	return ssoView(row), nil
}

// DeleteOrgSSO removes an organization's SSO configuration (admin only).
func (s *Service) DeleteOrgSSO(ctx context.Context, actor Actor, orgSlug string) error {
	tenant, err := s.getTenant(ctx, orgSlug)
	if err != nil {
		return err
	}
	if err := s.authorizeTenantAdmin(ctx, actor, tenant.ID); err != nil {
		return err
	}
	if err := s.store.DeleteTenantSSO(ctx, tenant.ID); err != nil {
		return fmt.Errorf("delete sso config: %w", err)
	}
	return nil
}

// ssoView projects a stored row into its API-facing form, never exposing the
// encrypted secret.
func ssoView(row db.TenantSsoConfig) SSOConfigView {
	return SSOConfigView{
		Configured:  true,
		Protocol:    row.Protocol,
		Issuer:      row.Issuer,
		ClientID:    row.ClientID,
		EmailDomain: row.EmailDomain,
		Enabled:     row.Enabled,
		HasSecret:   len(row.ClientSecretCiphertext) > 0,
		UpdatedAt:   row.UpdatedAt,
	}
}
