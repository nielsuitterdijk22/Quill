-- Live log lines of in-flight pipeline runs (2026-11-14). CI workers run on
-- separate machines and the api scales across instances, so live output is
-- buffered here (not in api memory) and every api instance can stream it to
-- viewers. Rows are deleted when the run's final result is persisted: the
-- step logs in pipeline_steps are the durable copy. Lines are already
-- secret-masked when inserted.
CREATE TABLE pipeline_live_logs (
    id         BIGSERIAL PRIMARY KEY,
    run_id     UUID NOT NULL REFERENCES pipeline_runs(id) ON DELETE CASCADE,
    job_key    TEXT NOT NULL,
    step_name  TEXT NOT NULL,
    line       TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX pipeline_live_logs_run_idx ON pipeline_live_logs (run_id, id);
