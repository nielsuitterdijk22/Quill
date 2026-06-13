-- name: CreateRepository :one
INSERT INTO repositories (
  org_id, owning_team_id, slug, name, description, visibility, default_branch
)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: GetRepositoryByID :one
SELECT * FROM repositories WHERE id = $1;

-- name: GetRepositoryBySlug :one
SELECT * FROM repositories
WHERE org_id = $1 AND lower(slug) = lower($2);

-- name: ListRepositoriesByOrg :many
SELECT * FROM repositories
WHERE org_id = $1
ORDER BY slug
LIMIT $2 OFFSET $3;

-- name: ListRepositoriesByTeam :many
SELECT * FROM repositories
WHERE owning_team_id = $1
ORDER BY slug;

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
