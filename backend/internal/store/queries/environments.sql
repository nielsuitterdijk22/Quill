-- name: CreateEnvironment :one
INSERT INTO environments (
  project_id, slug, name, description, rank
)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetEnvironmentByID :one
SELECT * FROM environments WHERE id = $1;

-- name: GetEnvironmentBySlug :one
SELECT * FROM environments
WHERE project_id = $1 AND lower(slug) = lower($2);

-- name: ListEnvironmentsByProject :many
SELECT * FROM environments
WHERE project_id = $1
ORDER BY rank, lower(slug);

-- name: UpdateEnvironment :one
UPDATE environments
SET name = $2,
    description = $3,
    rank = $4
WHERE id = $1
RETURNING *;

-- name: DeleteEnvironment :exec
DELETE FROM environments WHERE id = $1;
