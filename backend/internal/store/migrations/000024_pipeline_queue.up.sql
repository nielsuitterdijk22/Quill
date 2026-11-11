-- Pipeline execution queue (serverless epic, PR A1): decouples the api's
-- trigger path from the executor. In queue mode a trigger inserts a row here
-- carrying the full pipeline.JobSpec; a worker (the CI broker/job in later
-- PRs, the dispatch container meanwhile) claims rows with FOR UPDATE
-- SKIP LOCKED and reports results back through the api.
--
-- Correctness rules:
--   * one row = one pipeline run; claiming is exclusive (SKIP LOCKED + status)
--   * a claim lapses after claimed_at + visibility timeout (dead-worker
--     recovery) unless the worker heartbeats; attempts counts claim rounds
--   * tenant_id is stamped at enqueue for tenant-scoped metering (CI minutes,
--     storage, pushes) — nullable like audit_log for legacy/system rows
--   * spec is jsonb: the full JobSpec incl. resolved secrets. The trust
--     domain is the same as today (the spec already travels as JSON to the
--     dispatcher over HMAC); it is never returned to clients.
CREATE TABLE pipeline_queue (
  id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  run_id      uuid NOT NULL REFERENCES pipeline_runs(id) ON DELETE CASCADE,
  tenant_id   uuid REFERENCES tenants(id) ON DELETE SET NULL,
  spec        jsonb NOT NULL,
  status      text NOT NULL DEFAULT 'pending', -- 'pending' | 'claimed' | 'done'
  attempts    int  NOT NULL DEFAULT 0,
  claimed_by  text NOT NULL DEFAULT '',
  claimed_at  timestamptz,
  created_at  timestamptz NOT NULL DEFAULT now(),
  updated_at  timestamptz NOT NULL DEFAULT now()
);

-- Partial index: workers scan "not done, oldest first".
CREATE INDEX pipeline_queue_ready_idx ON pipeline_queue (created_at) WHERE status <> 'done';

CREATE TRIGGER pipeline_queue_set_updated_at BEFORE UPDATE ON pipeline_queue
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();
