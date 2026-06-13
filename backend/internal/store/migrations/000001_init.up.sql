-- Quill core schema.
--
-- Identity, namespaces, ownership, and audit. Forgejo owns git; these tables hold
-- the platform metadata Forgejo can't. Case-insensitive uniqueness is enforced
-- with lower() indexes (no citext extension), and the store normalizes on write.

BEGIN;

-- Auto-maintain updated_at on row updates.
CREATE OR REPLACE FUNCTION set_updated_at() RETURNS trigger AS $$
BEGIN
  NEW.updated_at = now();
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- ---- users: Quill identities -----------------------------------------------
CREATE TABLE users (
  id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  username         text NOT NULL,
  email            text NOT NULL,
  display_name     text NOT NULL DEFAULT '',
  is_admin         boolean NOT NULL DEFAULT false,
  is_active        boolean NOT NULL DEFAULT true,
  forgejo_user_id  bigint,
  forgejo_username text,
  created_at       timestamptz NOT NULL DEFAULT now(),
  updated_at       timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX users_username_lower_idx ON users (lower(username));
CREATE UNIQUE INDEX users_email_lower_idx ON users (lower(email));
CREATE TRIGGER users_set_updated_at BEFORE UPDATE ON users
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- ---- auth_identities: pluggable auth provider mapping -----------------------
-- One row per (provider, subject). Local provider stores a bcrypt hash in
-- secret_hash; OIDC providers (keycloak/entra/github) leave it NULL.
CREATE TABLE auth_identities (
  id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id     uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  provider    text NOT NULL,
  subject     text NOT NULL,
  secret_hash text,
  created_at  timestamptz NOT NULL DEFAULT now(),
  updated_at  timestamptz NOT NULL DEFAULT now(),
  UNIQUE (provider, subject)
);
CREATE INDEX auth_identities_user_id_idx ON auth_identities (user_id);
CREATE TRIGGER auth_identities_set_updated_at BEFORE UPDATE ON auth_identities
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- ---- organizations: top-level namespaces -----------------------------------
-- parent_id is reserved for nested groups later; it is NULL for flat orgs today.
CREATE TABLE organizations (
  id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  slug             text NOT NULL,
  name             text NOT NULL,
  description      text NOT NULL DEFAULT '',
  parent_id        uuid REFERENCES organizations(id) ON DELETE RESTRICT,
  forgejo_org_name text,
  created_at       timestamptz NOT NULL DEFAULT now(),
  updated_at       timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX organizations_slug_lower_idx ON organizations (lower(slug));
CREATE INDEX organizations_parent_id_idx ON organizations (parent_id);
CREATE TRIGGER organizations_set_updated_at BEFORE UPDATE ON organizations
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- ---- org membership --------------------------------------------------------
CREATE TABLE org_members (
  org_id     uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  user_id    uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  role       text NOT NULL DEFAULT 'member', -- 'owner' | 'member'
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (org_id, user_id)
);
CREATE INDEX org_members_user_id_idx ON org_members (user_id);

-- ---- teams: access + ownership unit within an org --------------------------
CREATE TABLE teams (
  id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id      uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  slug        text NOT NULL,
  name        text NOT NULL,
  description text NOT NULL DEFAULT '',
  created_at  timestamptz NOT NULL DEFAULT now(),
  updated_at  timestamptz NOT NULL DEFAULT now(),
  -- composite uniqueness lets repositories pin "owning team is in this org".
  UNIQUE (id, org_id)
);
CREATE UNIQUE INDEX teams_org_slug_lower_idx ON teams (org_id, lower(slug));
CREATE INDEX teams_org_id_idx ON teams (org_id);
CREATE TRIGGER teams_set_updated_at BEFORE UPDATE ON teams
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- ---- team membership -------------------------------------------------------
CREATE TABLE team_members (
  team_id    uuid NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
  user_id    uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  role       text NOT NULL DEFAULT 'member', -- 'maintainer' | 'member'
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (team_id, user_id)
);
CREATE INDEX team_members_user_id_idx ON team_members (user_id);

-- ---- repositories: every repo has a required owning team -------------------
-- ownership-as-data: owning_team_id is NOT NULL and (with the composite FK) must
-- belong to the repo's org. RESTRICT keeps orgs/teams from being deleted out
-- from under live repos.
CREATE TABLE repositories (
  id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id          uuid NOT NULL REFERENCES organizations(id) ON DELETE RESTRICT,
  owning_team_id  uuid NOT NULL,
  slug            text NOT NULL,
  name            text NOT NULL,
  description     text NOT NULL DEFAULT '',
  visibility      text NOT NULL DEFAULT 'private', -- 'public' | 'internal' | 'private'
  default_branch  text NOT NULL DEFAULT 'main',
  is_archived     boolean NOT NULL DEFAULT false,
  forgejo_repo_id bigint,
  forgejo_owner   text,
  forgejo_name    text,
  created_at      timestamptz NOT NULL DEFAULT now(),
  updated_at      timestamptz NOT NULL DEFAULT now(),
  FOREIGN KEY (owning_team_id, org_id) REFERENCES teams (id, org_id) ON DELETE RESTRICT
);
CREATE UNIQUE INDEX repositories_org_slug_lower_idx ON repositories (org_id, lower(slug));
CREATE INDEX repositories_org_id_idx ON repositories (org_id);
CREATE INDEX repositories_owning_team_id_idx ON repositories (owning_team_id);
CREATE TRIGGER repositories_set_updated_at BEFORE UPDATE ON repositories
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- ---- audit log -------------------------------------------------------------
CREATE TABLE audit_log (
  id            bigserial PRIMARY KEY,
  actor_user_id uuid REFERENCES users(id) ON DELETE SET NULL,
  action        text NOT NULL,
  target_type   text NOT NULL DEFAULT '',
  target_id     text NOT NULL DEFAULT '',
  metadata      jsonb NOT NULL DEFAULT '{}'::jsonb,
  created_at    timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX audit_log_actor_idx ON audit_log (actor_user_id);
CREATE INDEX audit_log_created_at_idx ON audit_log (created_at DESC);

COMMIT;
