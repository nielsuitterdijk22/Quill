-- Nested groups: departments and teams inside an org tenant.
--
-- A group tree hangs off a tenant (org): Group → parent Group, max depth 5
-- (depth 1 = root, e.g. a department; deeper levels are teams). Members join
-- groups directly; effective membership includes all ancestors, so a role
-- granted to a department group applies to every team beneath it.
-- group_project_roles grants a group a role on a project; the effective
-- project role of a user is the max of their direct project_members role and
-- the roles of all their effective groups (see GetEffectiveProjectRole).

BEGIN;

CREATE TABLE groups (
  id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id  uuid NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  parent_id  uuid REFERENCES groups(id) ON DELETE CASCADE,
  slug       text NOT NULL,
  name       text NOT NULL,
  -- depth 1 = root group; enforced <= 5 by the service layer.
  depth      integer NOT NULL DEFAULT 1,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT groups_slug_unique_per_tenant UNIQUE (tenant_id, slug),
  CONSTRAINT groups_depth_range CHECK (depth >= 1 AND depth <= 5)
);
CREATE INDEX groups_tenant_id_idx ON groups (tenant_id);
CREATE INDEX groups_parent_id_idx ON groups (parent_id);
CREATE TRIGGER groups_set_updated_at BEFORE UPDATE ON groups
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE group_members (
  group_id   uuid NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
  user_id    uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (group_id, user_id)
);
CREATE INDEX group_members_user_id_idx ON group_members (user_id);

-- Group → project role grants ('owner' | 'member'), inherited by every
-- effective member of the group (and, via ancestor expansion, of subgroups).
CREATE TABLE group_project_roles (
  group_id   uuid NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
  project_id uuid NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  role       text NOT NULL DEFAULT 'member', -- 'owner' | 'member'
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (group_id, project_id)
);
CREATE INDEX group_project_roles_project_id_idx ON group_project_roles (project_id);

COMMIT;
