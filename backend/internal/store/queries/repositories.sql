-- name: CreateRepository :one
INSERT INTO repositories (
  project_id, slug, name, description, visibility, default_branch
)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetRepositoryByID :one
SELECT * FROM repositories WHERE id = $1;

-- name: GetRepositoryBySlug :one
SELECT * FROM repositories
WHERE project_id = $1 AND lower(slug) = lower($2);

-- name: ListRepositoriesByProject :many
SELECT * FROM repositories
WHERE project_id = $1
ORDER BY slug
LIMIT $2 OFFSET $3;

-- name: SetRepositoryForgejoLink :one
UPDATE repositories
SET forgejo_repo_id = $2, forgejo_owner = $3, forgejo_name = $4
WHERE id = $1
RETURNING *;

-- name: SetRepositoryArchived :one
UPDATE repositories
SET is_archived = $2
WHERE id = $1
RETURNING *;

-- name: UpdateRepository :one
UPDATE repositories
SET slug = $2,
    name = $3,
    description = $4,
    visibility = $5,
    default_branch = $6,
    is_archived = $7,
    forgejo_name = $8
WHERE id = $1
RETURNING *;

-- name: DeleteRepository :exec
DELETE FROM repositories WHERE id = $1;
