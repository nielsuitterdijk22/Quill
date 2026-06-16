-- Quill core schema.
--
-- Identity, tenancy, namespaces, ownership, and audit. Forgejo owns git; these
-- tables hold the platform metadata Forgejo can't. The model is intentionally
-- flat for the MVP: Tenant > Project > Resource (repositories / pipelines).
-- A tenant is the billing / SSO boundary; a project is a team/app namespace and
-- can own multiple repositories and pipelines. Case-insensitive uniqueness is
-- enforced with lower() indexes (no citext extension); the store normalizes on
-- write.

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

-- ---- tenants: billing / SSO boundary ---------------------------------------
-- The top of the hierarchy. A tenant owns projects and is where billing and SSO
-- configuration will hang off later. The MVP ships a single seeded 'default'
-- tenant and has no tenant-management UI yet.
CREATE TABLE tenants (
  id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  slug        text NOT NULL,
  name        text NOT NULL,
  created_at  timestamptz NOT NULL DEFAULT now(),
  updated_at  timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX tenants_slug_lower_idx ON tenants (lower(slug));
CREATE TRIGGER tenants_set_updated_at BEFORE UPDATE ON tenants
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- The default tenant every project attaches to until multi-tenant UI lands.
INSERT INTO tenants (slug, name) VALUES ('default', 'Default');

-- ---- projects: team/app namespaces under a tenant --------------------------
-- A project groups repositories and pipelines for one team or app. The slug is
-- globally unique because a project maps 1:1 to a Forgejo org, whose handle
-- lives in a single global namespace.
CREATE TABLE projects (
  id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id        uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
  slug             text NOT NULL,
  name             text NOT NULL,
  description      text NOT NULL DEFAULT '',
  forgejo_org_name text,
  created_at       timestamptz NOT NULL DEFAULT now(),
  updated_at       timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX projects_slug_lower_idx ON projects (lower(slug));
CREATE INDEX projects_tenant_id_idx ON projects (tenant_id);
CREATE TRIGGER projects_set_updated_at BEFORE UPDATE ON projects
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- ---- project membership ----------------------------------------------------
CREATE TABLE project_members (
  project_id uuid NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  user_id    uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  role       text NOT NULL DEFAULT 'member', -- 'owner' | 'member'
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (project_id, user_id)
);
CREATE INDEX project_members_user_id_idx ON project_members (user_id);

-- ---- repositories: a project owns many repos -------------------------------
-- ownership-as-data: project_id is NOT NULL and RESTRICT keeps a project from
-- being deleted out from under live repos.
CREATE TABLE repositories (
  id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  project_id      uuid NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
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
  updated_at      timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX repositories_project_slug_lower_idx ON repositories (project_id, lower(slug));
CREATE INDEX repositories_project_id_idx ON repositories (project_id);
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
