BEGIN;

ALTER TABLE users DROP COLUMN IF EXISTS username_confirmed;
ALTER TABLE tenant_sso_config DROP COLUMN IF EXISTS external_idp_id;

COMMIT;
