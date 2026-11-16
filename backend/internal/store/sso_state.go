package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// SSOState is the IdP-side state of an org's SSO config (migration 000041):
// what Quill applied to Zitadel and the email-domain verification.
type SSOState struct {
	ZitadelOrgID       string
	ZitadelIdPID       string
	ZitadelIdPProtocol string
	// RoutedDomain is the domain currently registered on the Zitadel org
	// (i.e. actually routing logins); empty when none.
	RoutedDomain      string
	DomainVerifyToken string
	DomainVerifiedAt  *time.Time
	LastError         string
}

// GetSSOState reads a tenant's SSO state (zero value when no config row).
func (s *Store) GetSSOState(ctx context.Context, tenantID uuid.UUID) (SSOState, error) {
	var st SSOState
	err := s.pool.QueryRow(ctx, `
		SELECT zitadel_org_id, zitadel_idp_id, zitadel_idp_protocol, routed_domain, domain_verify_token, domain_verified_at, last_error
		FROM tenant_sso_config WHERE tenant_id = $1`, tenantID).
		Scan(&st.ZitadelOrgID, &st.ZitadelIdPID, &st.ZitadelIdPProtocol, &st.RoutedDomain, &st.DomainVerifyToken, &st.DomainVerifiedAt, &st.LastError)
	if errors.Is(err, pgx.ErrNoRows) {
		return SSOState{}, nil
	}
	return st, err
}

// SetSSOState replaces a tenant's SSO state (the config row must exist).
func (s *Store) SetSSOState(ctx context.Context, tenantID uuid.UUID, st SSOState) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE tenant_sso_config SET zitadel_org_id = $2, zitadel_idp_id = $3, zitadel_idp_protocol = $4,
		    routed_domain = $5, domain_verify_token = $6, domain_verified_at = $7, last_error = $8, updated_at = now()
		WHERE tenant_id = $1`,
		tenantID, st.ZitadelOrgID, st.ZitadelIdPID, st.ZitadelIdPProtocol, st.RoutedDomain, st.DomainVerifyToken, st.DomainVerifiedAt, st.LastError)
	return err
}

// DomainVerifiedByOtherTenant reports whether another tenant already owns a
// verified SSO domain.
func (s *Store) DomainVerifiedByOtherTenant(ctx context.Context, tenantID uuid.UUID, domain string) (bool, error) {
	var n int
	err := s.pool.QueryRow(ctx, `
		SELECT count(*) FROM tenant_sso_config
		WHERE lower(email_domain) = lower($2) AND domain_verified_at IS NOT NULL AND tenant_id <> $1`, tenantID, domain).Scan(&n)
	return n > 0, err
}

// TenantForSSOLogin maps the Zitadel org a sign-in came from to the Quill
// tenant whose enabled SSO owns it (ok=false: not an SSO org).
func (s *Store) TenantForSSOLogin(ctx context.Context, zitadelOrgID string) (uuid.UUID, bool, error) {
	if zitadelOrgID == "" {
		return uuid.Nil, false, nil
	}
	var id uuid.UUID
	err := s.pool.QueryRow(ctx, `
		SELECT tenant_id FROM tenant_sso_config WHERE zitadel_org_id = $1 AND enabled`, zitadelOrgID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, false, nil
	}
	return id, err == nil, err
}

// ListSSOZitadelOrgs returns the Zitadel org ids recorded for the given
// tenants (used to tear them down when e2e tenants are purged).
func (s *Store) ListSSOZitadelOrgs(ctx context.Context, tenantIDs []uuid.UUID) ([]string, error) {
	rows, err := s.pool.Query(ctx, `SELECT zitadel_org_id FROM tenant_sso_config WHERE tenant_id = ANY($1) AND zitadel_org_id <> ''`, tenantIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// SSOVerifiedDomain returns the verified, routed domain of the enabled SSO
// org a Zitadel org belongs to (ok=false: none).
func (s *Store) SSOVerifiedDomain(ctx context.Context, zitadelOrgID string) (string, bool, error) {
	if zitadelOrgID == "" {
		return "", false, nil
	}
	var d string
	err := s.pool.QueryRow(ctx, `
		SELECT email_domain FROM tenant_sso_config
		WHERE zitadel_org_id = $1 AND enabled AND domain_verified_at IS NOT NULL AND email_domain <> ''`, zitadelOrgID).Scan(&d)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	return d, err == nil, err
}
