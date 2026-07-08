-- name: AddTenantMember :exec
INSERT INTO tenant_members (tenant_id, user_id, role)
VALUES ($1, $2, $3)
ON CONFLICT (tenant_id, user_id) DO UPDATE SET role = EXCLUDED.role;

-- name: GetTenantMember :one
SELECT tenant_id, user_id, role, created_at FROM tenant_members
WHERE tenant_id = $1 AND user_id = $2;

-- name: ListTenantMembers :many
SELECT u.id, u.username, u.email, u.display_name, u.is_admin,
       m.role AS member_role, m.created_at AS member_since
FROM tenant_members m
JOIN users u ON u.id = m.user_id
WHERE m.tenant_id = $1
ORDER BY u.username;

-- name: RemoveTenantMember :exec
DELETE FROM tenant_members WHERE tenant_id = $1 AND user_id = $2;

-- name: CountTenantAdmins :one
SELECT count(*) FROM tenant_members WHERE tenant_id = $1 AND role = 'admin';
