-- SSO login (2026-11-16). An org's SSO config is applied to Zitadel: a
-- dedicated Zitadel org holding the customer's IdP (OIDC or SAML) behind an
-- SSO-only login policy. Its email domain routes logins there (Zitadel domain
-- discovery) only after Quill verified a DNS TXT record: Zitadel itself does
-- not validate org domains on this instance, so Quill is the gate.
ALTER TABLE tenant_sso_config
    ADD COLUMN zitadel_org_id       text NOT NULL DEFAULT '',
    ADD COLUMN zitadel_idp_id       text NOT NULL DEFAULT '',
    ADD COLUMN zitadel_idp_protocol text NOT NULL DEFAULT '',
    ADD COLUMN routed_domain        text NOT NULL DEFAULT '',
    ADD COLUMN domain_verify_token  text NOT NULL DEFAULT '',
    ADD COLUMN domain_verified_at   timestamptz,
    ADD COLUMN last_error           text NOT NULL DEFAULT '';

-- A verified domain belongs to exactly one organization.
CREATE UNIQUE INDEX tenant_sso_verified_domain ON tenant_sso_config (lower(email_domain))
    WHERE domain_verified_at IS NOT NULL;
CREATE INDEX tenant_sso_zitadel_org ON tenant_sso_config (zitadel_org_id) WHERE zitadel_org_id <> '';
