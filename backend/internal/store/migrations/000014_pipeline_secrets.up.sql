-- Pipeline secrets: encrypted key/value pairs exposed to CI workflows as
-- ${{ secrets.NAME }}. One table holds all three scopes, distinguished by which
-- foreign key is set:
--   * both repo_id and environment_id NULL -> project-wide secret
--   * repo_id set                          -> repository secret
--   * environment_id set                   -> environment secret
-- project_id is always set so a project delete cascades away every secret and
-- so run-time resolution can gather a project's whole secret set in one query.
-- At run time secrets merge project -> repo -> environment, later winning, so a
-- repo or environment secret overrides a project secret of the same name.
--
-- Values are encrypted at rest with AES-256-GCM (see internal/secretbox): only
-- the ciphertext and per-row nonce are stored, never the plaintext, and the API
-- never reads a value back — secrets are write-only after creation.

BEGIN;

CREATE TABLE pipeline_secrets (
  id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  project_id     uuid NOT NULL REFERENCES projects(id)     ON DELETE CASCADE,
  repo_id        uuid          REFERENCES repositories(id) ON DELETE CASCADE,
  environment_id uuid          REFERENCES environments(id) ON DELETE CASCADE,
  -- name follows GitHub Actions rules (uppercase/underscore); uniqueness is
  -- enforced per scope by the partial indexes below.
  name           text NOT NULL,
  ciphertext     bytea NOT NULL,
  nonce          bytea NOT NULL,
  created_by     uuid REFERENCES users(id) ON DELETE SET NULL,
  created_at     timestamptz NOT NULL DEFAULT now(),
  updated_at     timestamptz NOT NULL DEFAULT now(),
  -- A row belongs to exactly one scope: it never sets both foreign keys.
  CONSTRAINT pipeline_secrets_single_scope CHECK (repo_id IS NULL OR environment_id IS NULL)
);

-- A secret name is unique within its scope. Partial unique indexes model the
-- three scopes independently so, e.g., a project secret and a repo secret in
-- that project may share a name (the repo one overrides at run time).
CREATE UNIQUE INDEX pipeline_secrets_project_name_idx
  ON pipeline_secrets (project_id, name)
  WHERE repo_id IS NULL AND environment_id IS NULL;
CREATE UNIQUE INDEX pipeline_secrets_repo_name_idx
  ON pipeline_secrets (repo_id, name)
  WHERE repo_id IS NOT NULL;
CREATE UNIQUE INDEX pipeline_secrets_environment_name_idx
  ON pipeline_secrets (environment_id, name)
  WHERE environment_id IS NOT NULL;

CREATE INDEX pipeline_secrets_project_id_idx ON pipeline_secrets (project_id);
CREATE INDEX pipeline_secrets_repo_id_idx ON pipeline_secrets (repo_id) WHERE repo_id IS NOT NULL;
CREATE INDEX pipeline_secrets_environment_id_idx ON pipeline_secrets (environment_id) WHERE environment_id IS NOT NULL;

CREATE TRIGGER pipeline_secrets_set_updated_at BEFORE UPDATE ON pipeline_secrets
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();

COMMIT;
