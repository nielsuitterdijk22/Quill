DROP INDEX IF EXISTS tenant_sso_zitadel_org;
DROP INDEX IF EXISTS tenant_sso_verified_domain;
ALTER TABLE tenant_sso_config
    DROP COLUMN IF EXISTS zitadel_org_id,
    DROP COLUMN IF EXISTS zitadel_idp_id,
    DROP COLUMN IF EXISTS zitadel_idp_protocol,
    DROP COLUMN IF EXISTS routed_domain,
    DROP COLUMN IF EXISTS domain_verify_token,
    DROP COLUMN IF EXISTS domain_verified_at,
    DROP COLUMN IF EXISTS last_error;
