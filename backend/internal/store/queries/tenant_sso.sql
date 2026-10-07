-- name: GetTenantSSO :one
SELECT tenant_id, protocol, issuer, client_id, client_secret_ciphertext, client_secret_nonce, email_domain, enabled, created_at, updated_at, external_idp_id, domain_verification_type, domain_verification_token
FROM tenant_sso_config WHERE tenant_id = $1;

-- name: UpsertTenantSSO :one
-- external_idp_id is intentionally omitted: it is set separately after the config
-- is provisioned into Zitadel, and left untouched here so a config edit preserves
-- the existing IdP linkage.
INSERT INTO tenant_sso_config (
  tenant_id, protocol, issuer, client_id, client_secret_ciphertext, client_secret_nonce, email_domain, enabled
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
ON CONFLICT (tenant_id) DO UPDATE SET
  protocol = EXCLUDED.protocol,
  issuer = EXCLUDED.issuer,
  client_id = EXCLUDED.client_id,
  client_secret_ciphertext = EXCLUDED.client_secret_ciphertext,
  client_secret_nonce = EXCLUDED.client_secret_nonce,
  email_domain = EXCLUDED.email_domain,
  enabled = EXCLUDED.enabled
RETURNING tenant_id, protocol, issuer, client_id, client_secret_ciphertext, client_secret_nonce, email_domain, enabled, created_at, updated_at, external_idp_id, domain_verification_type, domain_verification_token;

-- name: SetTenantSSOExternalIDP :exec
UPDATE tenant_sso_config SET external_idp_id = $2 WHERE tenant_id = $1;

-- name: SetTenantSSODomainVerification :exec
-- Persists the generated domain-ownership challenge (type + token) so the same
-- DNS record is shown across page loads. Zitadel stays the source of truth for
-- whether the domain is actually verified; that flag is live-read, not stored.
UPDATE tenant_sso_config
SET domain_verification_type = $2, domain_verification_token = $3
WHERE tenant_id = $1;

-- name: DeleteTenantSSO :exec
DELETE FROM tenant_sso_config WHERE tenant_id = $1;

-- name: GetEnabledTenantSSOByDomain :one
-- Resolves an email domain to its SSO-enabled org for login-time discovery.
-- Only enabled configs with a provisioned backing org (external_org_id) match;
-- the unique index on (email_domain) WHERE enabled guarantees at most one row.
SELECT t.external_org_id, t.slug, t.name
FROM tenant_sso_config c
JOIN tenants t ON t.id = c.tenant_id
WHERE c.email_domain = $1 AND c.enabled
  AND t.external_org_id IS NOT NULL AND t.external_org_id <> '';

-- name: GetEnabledTenantSSOBySlug :one
-- Resolves an org slug to its SSO-enabled backing org for the workspace-slug
-- login path (no domain required).
SELECT t.external_org_id, t.slug, t.name
FROM tenant_sso_config c
JOIN tenants t ON t.id = c.tenant_id
WHERE t.slug = $1 AND c.enabled
  AND t.external_org_id IS NOT NULL AND t.external_org_id <> '';
