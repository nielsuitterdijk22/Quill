BEGIN;

ALTER TABLE tenant_sso_config
  DROP COLUMN domain_verification_type,
  DROP COLUMN domain_verification_token;

COMMIT;
