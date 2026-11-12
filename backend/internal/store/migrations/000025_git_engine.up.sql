-- Git engine referee + runtime routing hint (serverless epic, PR B1).
--
-- git_repo_state is THE correctness referee for ref updates in the own git
-- engine (roadmap irreversible decision #2): one row per repository carrying
-- the refs map and a monotonically increasing revision. Writers
-- compare-and-swap on revision (UPDATE ... WHERE revision = <expected>), so
-- exactly one winner exists per revision and the Postgres row is the commit
-- point — the bucket manifest is a reproducible view of this row.
--
-- tenant_id is stamped at provisioning (nullable like audit_log / the pipeline
-- queue: legacy or system rows may pre-date tenancy, and metering must never
-- depend on joins surviving tenant deletion).
CREATE TABLE git_repo_state (
  repo_id       uuid PRIMARY KEY REFERENCES repositories(id) ON DELETE CASCADE,
  tenant_id     uuid REFERENCES tenants(id) ON DELETE SET NULL,
  revision      bigint NOT NULL DEFAULT 1,
  refs          jsonb NOT NULL DEFAULT '{}'::jsonb,
  storage_bytes bigint NOT NULL DEFAULT 0,
  updated_at    timestamptz NOT NULL DEFAULT now()
);

-- Runtime routing hint (roadmap scale-out amendment, "design now, implement
-- later"): 'serverless' today; 'warm' routes the tenant's repos to warm-pool
-- runtimes later. Nothing reads this column yet — deliberately.
ALTER TABLE tenants ADD COLUMN runtime_hint text NOT NULL DEFAULT 'serverless';

CREATE TRIGGER git_repo_state_set_updated_at BEFORE UPDATE ON git_repo_state
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();
