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
-- Projects the user can reach through membership: a direct project_members row
-- or membership in the project's org tenant (tenant_members). This is the
-- visibility scope of the cross-cutting user views (PRs overview, dashboard
-- counts); platform admins are deliberately NOT broadened here — their user
-- views show their own memberships, admin tooling uses other paths.
SELECT DISTINCT p.*
FROM projects p
WHERE EXISTS (
    SELECT 1 FROM project_members pm
    WHERE pm.project_id = p.id AND pm.user_id = $1
  )
   OR EXISTS (
    SELECT 1 FROM tenant_members tm
    WHERE tm.tenant_id = p.tenant_id AND tm.user_id = $1
  )
ORDER BY p.slug;

-- name: ListVisibleProjectsForUserWithRole :many
-- Same memberships union as ListVisibleProjectsForUser, but also computes the
-- caller's effective role on each project (direct project_members role, else
-- the org-tenant membership with 'admin' -> owner), matching the precedence of
-- GetEffectiveProjectRole. Backs /me/projects (the project switcher).
SELECT p.id, p.tenant_id, p.slug, p.name, p.description, p.forgejo_org_name, p.created_at, p.updated_at, p.is_personal,
(
  SELECT r.role FROM (
    SELECT pm.role FROM project_members pm
    WHERE pm.project_id = p.id AND pm.user_id = $1
    UNION ALL
    SELECT CASE tm.role WHEN 'admin' THEN 'owner' ELSE 'member' END
    FROM tenant_members tm
    WHERE tm.tenant_id = p.tenant_id AND tm.user_id = $1
  ) r
  ORDER BY CASE r.role WHEN 'owner' THEN 2 WHEN 'member' THEN 1 ELSE 0 END DESC
  LIMIT 1
) AS member_role
FROM projects p
WHERE EXISTS (
    SELECT 1 FROM project_members pm
    WHERE pm.project_id = p.id AND pm.user_id = $1
  )
   OR EXISTS (
    SELECT 1 FROM tenant_members tm
    WHERE tm.tenant_id = p.tenant_id AND tm.user_id = $1
  )
ORDER BY p.slug;

-- name: ListProjectsByTenantID :many
SELECT * FROM projects
WHERE tenant_id = $1
ORDER BY slug;
