-- Deploy gates (2026-11-15): which environment a run deployed to, and why a
-- gated run was blocked. A side table keeps pipeline_runs (sqlc) untouched.
CREATE TABLE pipeline_run_environments (
    run_id         uuid PRIMARY KEY REFERENCES pipeline_runs(id) ON DELETE CASCADE,
    environment_id uuid REFERENCES environments(id) ON DELETE SET NULL,
    environment    text NOT NULL,
    blocked_reason text NOT NULL DEFAULT '',
    created_at     timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX pipeline_run_environments_env ON pipeline_run_environments (environment_id);
