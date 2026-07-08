-- name: GetTenantSSO :one
SELECT tenant_id, protocol, issuer, client_id, client_secret_ciphertext, client_secret_nonce, email_domain, enabled, created_at, updated_at
FROM tenant_sso_config WHERE tenant_id = $1;

-- name: UpsertTenantSSO :one
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
RETURNING tenant_id, protocol, issuer, client_id, client_secret_ciphertext, client_secret_nonce, email_domain, enabled, created_at, updated_at;

-- name: DeleteTenantSSO :exec
DELETE FROM tenant_sso_config WHERE tenant_id = $1;
