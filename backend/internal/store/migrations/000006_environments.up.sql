-- Environments: project-owned, ranked deployment targets (e.g. staging,
-- production). Quill owns these as platform metadata; Forgejo has no equivalent.
--
-- An environment is a named destination a repository's commit can be deployed
-- to. Environments belong to a project so every repository in the project shares
-- the same promotion ladder, and `rank` orders that ladder (lower deploys
-- first, e.g. staging rank 0 then production rank 1) so environment policies can
-- require an ordered promotion path. The `slug` is the stable identifier used in
-- URLs and matched by environment-policy selectors; `name` is the display label.

BEGIN;

CREATE TABLE environments (
  id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  project_id  uuid NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  slug        text NOT NULL,
  name        text NOT NULL,
  description text NOT NULL DEFAULT '',
  -- Promotion order within the project: lower ranks are earlier in the ladder.
  rank        integer NOT NULL DEFAULT 0,
  created_at  timestamptz NOT NULL DEFAULT now(),
  updated_at  timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX environments_project_slug_lower_idx ON environments (project_id, lower(slug));
CREATE INDEX environments_project_id_idx ON environments (project_id);
CREATE TRIGGER environments_set_updated_at BEFORE UPDATE ON environments
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();

COMMIT;
