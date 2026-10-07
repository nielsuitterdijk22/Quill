-- Enforce at most one enabled SSO config per email domain.
--
-- Login-time domain discovery (POST /api/v1/auth/sso/discover) maps an email
-- domain to a single organization's IdP. Two organizations claiming the same
-- domain would make that mapping ambiguous and let one capture another's login
-- routing, so a partial unique index guarantees the domain resolves to exactly
-- one org. Disabled configs and the empty domain are exempt.

BEGIN;

CREATE UNIQUE INDEX tenant_sso_config_email_domain_enabled_uidx
  ON tenant_sso_config (email_domain)
  WHERE enabled AND email_domain <> '';

COMMIT;
