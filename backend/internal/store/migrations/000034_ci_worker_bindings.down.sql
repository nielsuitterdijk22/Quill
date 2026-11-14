DROP TABLE IF EXISTS ci_worker_bindings;
ALTER TABLE pipeline_queue DROP COLUMN IF EXISTS ci_scope;
