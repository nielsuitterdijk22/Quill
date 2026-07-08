-- Organizations as first-class tenants.
--
-- Until now "signing up as an org" just created a project under the user's
-- personal tenant — there was no org entity, no org-admin role, and nowhere to
-- hang members or SSO. A tenant has always been described as the billing / SSO
-- boundary (000001_init); this migration lets a tenant *be* an organization: a
-- shared workspace with its own admins and members, distinct from the per-account
-- personal tenant every user receives at registration.

BEGIN;

-- kind distinguishes an org tenant (shared workspace) from a personal tenant.
-- Existing tenants (personal accounts and the seeded 'default') stay 'personal'.
ALTER TABLE tenants ADD COLUMN kind text NOT NULL DEFAULT 'personal';

-- Org membership and role. This is the org-level access boundary — a member's
-- role governs org administration (settings, members, SSO, org-wide policies) —
-- and is deliberately separate from project_members, which governs repo and
-- pipeline access within a single project.
CREATE TABLE tenant_members (
  tenant_id  uuid NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  user_id    uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  role       text NOT NULL DEFAULT 'member', -- 'admin' | 'member'
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, user_id)
);
CREATE INDEX tenant_members_user_id_idx ON tenant_members (user_id);

COMMIT;
