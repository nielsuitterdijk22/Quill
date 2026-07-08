-- Per-organization SSO configuration.
--
-- A tenant has always been described as the SSO boundary; this stores the actual
-- config an org admin arranges. The client secret is encrypted at rest with the
-- same secretbox cipher as pipeline secrets (ciphertext + nonce), and is
-- write-only over the API — reads only report whether a secret is set.
--
-- This is the configuration + storage surface. Consuming it to drive per-org
-- login selection lives in the auth layer (AuthProvider) as a follow-up; the
-- email_domain is the intended routing key.

BEGIN;

CREATE TABLE tenant_sso_config (
  tenant_id                uuid PRIMARY KEY REFERENCES tenants(id) ON DELETE CASCADE,
  protocol                 text NOT NULL DEFAULT 'oidc', -- 'oidc' | 'saml'
  issuer                   text NOT NULL DEFAULT '',      -- OIDC issuer or SAML metadata URL
  client_id                text NOT NULL DEFAULT '',
  client_secret_ciphertext bytea,
  client_secret_nonce      bytea,
  email_domain             text NOT NULL DEFAULT '',
  enabled                  boolean NOT NULL DEFAULT false,
  created_at               timestamptz NOT NULL DEFAULT now(),
  updated_at               timestamptz NOT NULL DEFAULT now()
);

CREATE TRIGGER tenant_sso_config_set_updated_at BEFORE UPDATE ON tenant_sso_config
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();

COMMIT;
