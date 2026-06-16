-- name: UpsertPipeline :one
INSERT INTO pipelines (repo_id, workflow_path, name)
VALUES ($1, $2, $3)
ON CONFLICT (repo_id, workflow_path) DO UPDATE SET
  name = EXCLUDED.name
RETURNING *;

-- name: GetPipeline :one
SELECT * FROM pipelines
WHERE id = $1;

-- name: GetPipelineByPath :one
SELECT * FROM pipelines
WHERE repo_id = $1 AND workflow_path = $2;

-- name: ListPipelinesByRepo :many
SELECT * FROM pipelines
WHERE repo_id = $1
ORDER BY workflow_path;

-- name: NextRunNumber :one
SELECT COALESCE(MAX(run_number), 0) + 1 AS next
FROM pipeline_runs
WHERE pipeline_id = $1;

-- name: CreatePipelineRun :one
INSERT INTO pipeline_runs (
  pipeline_id, run_number, status, event, ref, commit_sha, triggered_by, started_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: GetPipelineRun :one
SELECT * FROM pipeline_runs
WHERE id = $1;

-- name: GetPipelineRunByNumber :one
SELECT * FROM pipeline_runs
WHERE pipeline_id = $1 AND run_number = $2;

-- name: ListRunsByPipeline :many
SELECT * FROM pipeline_runs
WHERE pipeline_id = $1
ORDER BY run_number DESC
LIMIT $2;

-- name: ListRunsByRepo :many
SELECT r.* FROM pipeline_runs r
JOIN pipelines p ON p.id = r.pipeline_id
WHERE p.repo_id = $1
ORDER BY r.created_at DESC
LIMIT $2;

-- name: UpdatePipelineRunStatus :one
UPDATE pipeline_runs
SET status = $2, finished_at = $3
WHERE id = $1
RETURNING *;

-- name: CreatePipelineJob :one
INSERT INTO pipeline_jobs (
  run_id, job_key, name, runs_on, status, position, started_at, finished_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: ListJobsByRun :many
SELECT * FROM pipeline_jobs
WHERE run_id = $1
ORDER BY position;

-- name: CreatePipelineStep :one
INSERT INTO pipeline_steps (
  job_id, position, name, step_type, status, logs, started_at, finished_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: ListStepsByJob :many
SELECT * FROM pipeline_steps
WHERE job_id = $1
ORDER BY position;
