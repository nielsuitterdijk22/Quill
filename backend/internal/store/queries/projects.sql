-- name: CreateProject :one
INSERT INTO projects (tenant_id, slug, name, description, is_personal)
VALUES ($1, $2, $3, $4, $5)
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

-- name: DeleteProject :exec
DELETE FROM projects WHERE id = $1;

-- name: SetProjectForgejoName :one
UPDATE projects
SET forgejo_org_name = $2
WHERE id = $1
RETURNING *;

-- name: UpdateProjectSlug :one
UPDATE projects SET slug = $2, updated_at = now() WHERE id = $1 RETURNING *;

-- name: ListVisibleProjectsForUser :many
-- Projects the user can reach, from the one definition of access
-- (project_access, migration 000043). This is the visibility scope of the
-- cross-cutting user views (PRs overview, dashboard counts); platform admins
-- are deliberately NOT broadened here — their user views show their own
-- access, admin tooling uses other paths.
SELECT p.*
FROM projects p
JOIN project_access($1::uuid) a ON a.project_id = p.id
ORDER BY p.slug;

-- name: ListVisibleProjectsForUserWithRole :many
-- Same set as ListVisibleProjectsForUser, with the caller's effective role on
-- each project (the same project_access row GetEffectiveProjectRole reads).
-- Backs /me/projects (the project switcher).
SELECT p.id, p.tenant_id, p.slug, p.name, p.description, p.forgejo_org_name, p.created_at, p.updated_at, p.is_personal,
       a.role AS member_role
FROM projects p
JOIN project_access($1::uuid) a ON a.project_id = p.id
ORDER BY p.slug;

-- name: ListProjectsByTenantID :many
SELECT * FROM projects
WHERE tenant_id = $1
ORDER BY slug;
