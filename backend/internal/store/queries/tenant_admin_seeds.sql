-- name: AddTenantAdminSeed :exec
INSERT INTO tenant_admin_seeds (tenant_id, email) VALUES ($1, lower($2))
ON CONFLICT (tenant_id, email) DO NOTHING;

-- name: IsTenantAdminSeeded :one
SELECT EXISTS(SELECT 1 FROM tenant_admin_seeds WHERE tenant_id = $1 AND email = lower($2));
