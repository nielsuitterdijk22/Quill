-- SCIM 2.0 provisioning (2026-11-16). An org's identity provider (Okta, Entra
-- ID, Google, JumpCloud…) manages its members and groups through
-- /scim/v2 with a per-org bearer token.
--
-- scim_tokens: hashed bearer tokens (SHA-256 of qsc_<random>), one tenant each.
CREATE TABLE scim_tokens (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id    uuid NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    name         text NOT NULL DEFAULT '',
    token_hash   text NOT NULL UNIQUE,
    created_by   uuid REFERENCES users(id) ON DELETE SET NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    last_used_at timestamptz
);
CREATE INDEX scim_tokens_tenant ON scim_tokens (tenant_id);

-- scim_users: the IdP's view of a person in one org. The SCIM id is this row's
-- id (never the global user id), so one tenant's provisioning can never
-- address another tenant's users. Profile fields live here: SCIM never
-- rewrites a person's global Quill profile or email.
CREATE TABLE scim_users (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id    uuid NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    user_id      uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    user_name    text NOT NULL,
    external_id  text NOT NULL DEFAULT '',
    email        text NOT NULL DEFAULT '',
    given_name   text NOT NULL DEFAULT '',
    family_name  text NOT NULL DEFAULT '',
    display_name text NOT NULL DEFAULT '',
    active       boolean NOT NULL DEFAULT true,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, user_id)
);
CREATE UNIQUE INDEX scim_users_tenant_user_name ON scim_users (tenant_id, lower(user_name));

-- scim_groups: Quill groups the IdP owns (flat, depth 1).
CREATE TABLE scim_groups (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id    uuid NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    group_id     uuid NOT NULL UNIQUE REFERENCES groups(id) ON DELETE CASCADE,
    display_name text NOT NULL,
    external_id  text NOT NULL DEFAULT '',
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX scim_groups_tenant_name ON scim_groups (tenant_id, lower(display_name));
