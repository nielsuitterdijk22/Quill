-- name: AddOrgMember :exec
INSERT INTO org_members (org_id, user_id, role)
VALUES ($1, $2, $3)
ON CONFLICT (org_id, user_id) DO UPDATE SET role = EXCLUDED.role;

-- name: RemoveOrgMember :exec
DELETE FROM org_members WHERE org_id = $1 AND user_id = $2;

-- name: ListOrgMembers :many
SELECT u.*, m.role AS member_role
FROM org_members m
JOIN users u ON u.id = m.user_id
WHERE m.org_id = $1
ORDER BY u.username;

-- name: AddTeamMember :exec
INSERT INTO team_members (team_id, user_id, role)
VALUES ($1, $2, $3)
ON CONFLICT (team_id, user_id) DO UPDATE SET role = EXCLUDED.role;

-- name: RemoveTeamMember :exec
DELETE FROM team_members WHERE team_id = $1 AND user_id = $2;

-- name: ListTeamMembers :many
SELECT u.*, m.role AS member_role
FROM team_members m
JOIN users u ON u.id = m.user_id
WHERE m.team_id = $1
ORDER BY u.username;
