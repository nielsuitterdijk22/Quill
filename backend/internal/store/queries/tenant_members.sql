-- name: AddTenantMember :exec
INSERT INTO tenant_members (tenant_id, user_id, role)
VALUES ($1, $2, $3)
ON CONFLICT (tenant_id, user_id) DO UPDATE SET role = EXCLUDED.role;

-- name: GetTenantMember :one
SELECT tenant_id, user_id, role, created_at FROM tenant_members
WHERE tenant_id = $1 AND user_id = $2;
