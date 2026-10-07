-- Pre-seeded org admins for SSO onboarding.
--
-- When a platform admin provisions an SSO customer, the customer's designated
-- admins are created as credential-less user shells in the customer's Zitadel org
-- and granted ORG_OWNER there. This table records those same emails on the Quill
-- side so that, on the admin's first SSO login, JIT membership grants them the
-- Quill tenant 'admin' role (mirroring their Zitadel ORG_OWNER) instead of the
-- default 'member'. A row is a hint consumed at login; it is harmless to keep.

BEGIN;

CREATE TABLE tenant_admin_seeds (
  tenant_id  uuid NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  email      text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, email)
);

COMMIT;
