-- Per-account tenant isolation.
--
-- Quill is multi-tenant, but local-auth users were all provisioned into one
-- shared seeded 'default' tenant. Because a repository inherits its project's
-- tenant policies, a tenant-scoped policy set for the default tenant applied to
-- every local user's repositories — cross-account bleed that is unacceptable for
-- a SaaS. This migration gives each local account its own tenant.
--
-- users.tenant_id is the account's owning tenant. It is authoritative for local
-- users; Zitadel users keep it NULL and resolve their tenant per request from
-- the org claim in their token (one tenant per org), so this column never
-- overrides that path.

BEGIN;

ALTER TABLE users ADD COLUMN tenant_id uuid REFERENCES tenants(id);

-- Give every existing local user their own tenant. The slug mirrors the
-- username (globally unique and lower-unique, matching tenants' lower(slug)
-- index and the personal-project slug convention). Skip any user that already
-- has a tenant, and never collide with an existing tenant slug (e.g. 'default').
INSERT INTO tenants (slug, name)
SELECT u.username, COALESCE(NULLIF(u.display_name, ''), u.username)
FROM users u
WHERE u.tenant_id IS NULL
  AND EXISTS (
    SELECT 1 FROM auth_identities ai
    WHERE ai.user_id = u.id AND ai.provider = 'local'
  )
  AND NOT EXISTS (
    SELECT 1 FROM tenants t WHERE lower(t.slug) = lower(u.username)
  );

UPDATE users u
SET tenant_id = t.id
FROM tenants t
WHERE u.tenant_id IS NULL
  AND lower(t.slug) = lower(u.username)
  AND EXISTS (
    SELECT 1 FROM auth_identities ai
    WHERE ai.user_id = u.id AND ai.provider = 'local'
  );

-- Move each local user's owned projects out of the shared default tenant into
-- their own tenant, so their repositories stop inheriting the default tenant's
-- policies. A project with multiple owners resolves to one of them arbitrarily;
-- that is acceptable because tenant membership is not the access boundary
-- (project_members is) — this only decides which tenant's policies it inherits.
UPDATE projects p
SET tenant_id = u.tenant_id
FROM project_members m
JOIN users u ON u.id = m.user_id
WHERE m.project_id = p.id
  AND m.role = 'owner'
  AND u.tenant_id IS NOT NULL
  AND p.tenant_id = (SELECT id FROM tenants WHERE lower(slug) = 'default');

COMMIT;
