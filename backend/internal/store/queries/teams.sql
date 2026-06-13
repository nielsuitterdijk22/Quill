-- name: CreateTeam :one
INSERT INTO teams (org_id, slug, name, description)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: GetTeamByID :one
SELECT * FROM teams WHERE id = $1;

-- name: GetTeamBySlug :one
SELECT * FROM teams
WHERE org_id = $1 AND lower(slug) = lower($2);

-- name: ListTeamsByOrg :many
SELECT * FROM teams
WHERE org_id = $1
ORDER BY slug;
