-- name: CreatePipelineSecret :one
INSERT INTO pipeline_secrets (
  project_id, repo_id, environment_id, name, ciphertext, nonce, created_by
)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: UpdatePipelineSecretValue :one
UPDATE pipeline_secrets
SET ciphertext = $2,
    nonce = $3
WHERE id = $1
RETURNING *;

-- name: DeletePipelineSecret :exec
DELETE FROM pipeline_secrets WHERE id = $1;

-- name: GetProjectSecretByName :one
SELECT * FROM pipeline_secrets
WHERE project_id = $1 AND repo_id IS NULL AND environment_id IS NULL AND name = $2;

-- name: GetRepoSecretByName :one
SELECT * FROM pipeline_secrets
WHERE repo_id = $1 AND name = $2;

-- name: GetEnvironmentSecretByName :one
SELECT * FROM pipeline_secrets
WHERE environment_id = $1 AND name = $2;

-- name: ListProjectSecrets :many
SELECT * FROM pipeline_secrets
WHERE project_id = $1 AND repo_id IS NULL AND environment_id IS NULL
ORDER BY name;

-- name: ListRepoSecrets :many
SELECT * FROM pipeline_secrets
WHERE repo_id = $1
ORDER BY name;

-- name: ListEnvironmentSecrets :many
SELECT * FROM pipeline_secrets
WHERE environment_id = $1
ORDER BY name;
