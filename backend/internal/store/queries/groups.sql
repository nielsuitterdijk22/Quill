-- name: CreateGroup :one
INSERT INTO groups (tenant_id, parent_id, slug, name, depth)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetGroupBySlug :one
SELECT * FROM groups WHERE tenant_id = $1 AND lower(slug) = lower($2);

-- name: GetGroupByID :one
SELECT * FROM groups WHERE id = $1;

-- name: ListGroupsByTenant :many
SELECT * FROM groups WHERE tenant_id = $1 ORDER BY depth, slug;

-- name: UpdateGroup :one
UPDATE groups SET name = $2, slug = $3 WHERE id = $1 RETURNING *;

-- name: DeleteGroup :exec
DELETE FROM groups WHERE id = $1;

-- name: AddGroupMember :exec
INSERT INTO group_members (group_id, user_id)
VALUES ($1, $2)
ON CONFLICT (group_id, user_id) DO NOTHING;

-- name: RemoveGroupMember :exec
DELETE FROM group_members WHERE group_id = $1 AND user_id = $2;

-- name: ListGroupMembers :many
SELECT u.*, gm.created_at AS member_since
FROM group_members gm
JOIN users u ON u.id = gm.user_id
WHERE gm.group_id = $1
ORDER BY u.username;

-- name: ListGroupsForUser :many
-- Direct memberships only; ancestors are expanded by the recursive query below.
SELECT g.* FROM groups g
JOIN group_members gm ON gm.group_id = g.id
WHERE gm.user_id = $1 AND g.tenant_id = $2
ORDER BY g.depth, g.slug;

-- name: SetGroupProjectRole :exec
INSERT INTO group_project_roles (group_id, project_id, role)
VALUES ($1, $2, $3)
ON CONFLICT (group_id, project_id) DO UPDATE SET role = EXCLUDED.role;

-- name: RemoveGroupProjectRole :exec
DELETE FROM group_project_roles WHERE group_id = $1 AND project_id = $2;

-- name: ListGroupProjectRoles :many
SELECT gpr.*, p.slug AS project_slug
FROM group_project_roles gpr
JOIN projects p ON p.id = gpr.project_id
WHERE gpr.group_id = $1
ORDER BY p.slug;

-- name: ListProjectGroupRoles :many
SELECT gpr.*, g.slug AS group_slug, g.name AS group_name, g.depth AS group_depth
FROM group_project_roles gpr
JOIN groups g ON g.id = gpr.group_id
WHERE gpr.project_id = $1
ORDER BY g.depth, g.slug;

-- name: GetEffectiveProjectRole :one
-- The user's effective role on a project: max of their direct project_members
-- role, the roles granted to any of their effective groups (direct groups
-- plus all ancestors, via recursive expansion), and their tenant (org)
-- membership — org members can access their org's projects (tenant 'admin'
-- maps to project 'owner'). Returns '' when the user has no access.
-- Role precedence: owner > member.
WITH RECURSIVE effective_groups AS (
  SELECT gm.group_id
  FROM group_members gm
  WHERE gm.user_id = $2
  UNION
  SELECT g.parent_id
  FROM groups g
  JOIN effective_groups eg ON g.id = eg.group_id
  WHERE g.parent_id IS NOT NULL
),
candidates AS (
  SELECT pm.role FROM project_members pm
  WHERE pm.project_id = $1 AND pm.user_id = $2
  UNION ALL
  SELECT gpr.role
  FROM group_project_roles gpr
  JOIN effective_groups eg ON eg.group_id = gpr.group_id
  WHERE gpr.project_id = $1
  UNION ALL
  SELECT CASE WHEN tm.role = 'admin' THEN 'owner' ELSE 'member' END
  FROM tenant_members tm
  JOIN projects p ON p.tenant_id = tm.tenant_id
  WHERE p.id = $1 AND tm.user_id = $2
)
SELECT COALESCE((
  SELECT role FROM candidates
  ORDER BY CASE role WHEN 'owner' THEN 2 WHEN 'member' THEN 1 ELSE 0 END DESC
  LIMIT 1
), '') AS role;
