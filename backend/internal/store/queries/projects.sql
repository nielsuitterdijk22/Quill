-- name: CreateProject :one
INSERT INTO projects (tenant_id, slug, name, description)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: GetProjectByID :one
SELECT * FROM projects WHERE id = $1;

-- name: GetProjectBySlug :one
SELECT * FROM projects WHERE lower(slug) = lower($1);

-- name: ListProjects :many
SELECT * FROM projects
ORDER BY slug
LIMIT $1 OFFSET $2;

-- name: ListProjectsByUser :many
SELECT p.*, m.role AS member_role
FROM project_members m
JOIN projects p ON p.id = m.project_id
WHERE m.user_id = $1
ORDER BY p.slug;

-- name: CountProjects :one
SELECT count(*) FROM projects;

-- name: SetProjectForgejoName :one
UPDATE projects
SET forgejo_org_name = $2
WHERE id = $1
RETURNING *;
