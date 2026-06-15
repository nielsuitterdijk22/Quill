-- Pipelines (CI): Quill-owned execution records for GitHub Actions-style
-- workflows discovered in a repository's .github/workflows directory.
--
-- Quill is the source of truth for runs: it reads the workflow YAML from Forgejo
-- (via the contents API), interprets it through a pluggable Runner (nektos/act
-- today, Forge later), and records the run/job/step tree plus captured logs here.
-- Forgejo owns git; these tables hold the platform CI metadata it can't.

BEGIN;

-- ---- pipelines: one row per (repo, workflow file) --------------------------
-- A pipeline is the durable handle for a workflow file under .github/workflows.
-- It is created lazily the first time the workflow is run or observed.
CREATE TABLE pipelines (
  id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  repo_id       uuid NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
  -- Repo-relative path of the workflow file, e.g. '.github/workflows/ci.yml'.
  workflow_path text NOT NULL,
  -- Display name from the workflow's `name:` field, falling back to the file name.
  name          text NOT NULL DEFAULT '',
  created_at    timestamptz NOT NULL DEFAULT now(),
  updated_at    timestamptz NOT NULL DEFAULT now(),
  UNIQUE (repo_id, workflow_path)
);
CREATE INDEX pipelines_repo_id_idx ON pipelines (repo_id);
CREATE TRIGGER pipelines_set_updated_at BEFORE UPDATE ON pipelines
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- ---- pipeline_runs: one execution of a pipeline ----------------------------
-- status is one of: pending | running | success | failure | cancelled. A run is
-- triggered manually ('manual') or by a Forgejo webhook event ('push' | 'pull_request').
CREATE TABLE pipeline_runs (
  id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  pipeline_id   uuid NOT NULL REFERENCES pipelines(id) ON DELETE CASCADE,
  -- Monotonic per-pipeline run number for stable, human-friendly URLs.
  run_number    bigint NOT NULL,
  status        text NOT NULL DEFAULT 'pending',
  event         text NOT NULL DEFAULT 'manual',
  ref           text NOT NULL DEFAULT '',
  commit_sha    text NOT NULL DEFAULT '',
  -- Quill user who triggered the run (NULL for webhook-driven runs).
  triggered_by  uuid REFERENCES users(id) ON DELETE SET NULL,
  started_at    timestamptz,
  finished_at   timestamptz,
  created_at    timestamptz NOT NULL DEFAULT now(),
  updated_at    timestamptz NOT NULL DEFAULT now(),
  UNIQUE (pipeline_id, run_number)
);
CREATE INDEX pipeline_runs_pipeline_id_idx ON pipeline_runs (pipeline_id, run_number DESC);
CREATE TRIGGER pipeline_runs_set_updated_at BEFORE UPDATE ON pipeline_runs
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- ---- pipeline_jobs: a job within a run --------------------------------------
CREATE TABLE pipeline_jobs (
  id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  run_id      uuid NOT NULL REFERENCES pipeline_runs(id) ON DELETE CASCADE,
  -- Job key from the workflow YAML (the map key under `jobs:`).
  job_key     text NOT NULL,
  name        text NOT NULL DEFAULT '',
  runs_on     text NOT NULL DEFAULT '',
  status      text NOT NULL DEFAULT 'pending',
  -- Ordering index so jobs render in workflow order.
  position    integer NOT NULL DEFAULT 0,
  started_at  timestamptz,
  finished_at timestamptz,
  created_at  timestamptz NOT NULL DEFAULT now(),
  updated_at  timestamptz NOT NULL DEFAULT now(),
  UNIQUE (run_id, job_key)
);
CREATE INDEX pipeline_jobs_run_id_idx ON pipeline_jobs (run_id, position);
CREATE TRIGGER pipeline_jobs_set_updated_at BEFORE UPDATE ON pipeline_jobs
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- ---- pipeline_steps: a step within a job, with its captured logs -------------
CREATE TABLE pipeline_steps (
  id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  job_id      uuid NOT NULL REFERENCES pipeline_jobs(id) ON DELETE CASCADE,
  position    integer NOT NULL DEFAULT 0,
  name        text NOT NULL DEFAULT '',
  -- 'run' for shell steps, 'uses' for action steps.
  step_type   text NOT NULL DEFAULT 'run',
  status      text NOT NULL DEFAULT 'pending',
  -- Captured stdout/stderr for the step (bounded by the runner).
  logs        text NOT NULL DEFAULT '',
  started_at  timestamptz,
  finished_at timestamptz,
  created_at  timestamptz NOT NULL DEFAULT now(),
  updated_at  timestamptz NOT NULL DEFAULT now(),
  UNIQUE (job_id, position)
);
CREATE INDEX pipeline_steps_job_id_idx ON pipeline_steps (job_id, position);
CREATE TRIGGER pipeline_steps_set_updated_at BEFORE UPDATE ON pipeline_steps
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();

COMMIT;
