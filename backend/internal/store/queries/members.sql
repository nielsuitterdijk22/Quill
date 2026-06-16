-- name: AddProjectMember :exec
INSERT INTO project_members (project_id, user_id, role)
VALUES ($1, $2, $3)
ON CONFLICT (project_id, user_id) DO UPDATE SET role = EXCLUDED.role;

-- name: RemoveProjectMember :exec
DELETE FROM project_members WHERE project_id = $1 AND user_id = $2;

-- name: ListProjectMembers :many
SELECT u.*, m.role AS member_role
FROM project_members m
JOIN users u ON u.id = m.user_id
WHERE m.project_id = $1
ORDER BY u.username;
