-- name: GetTenantBySlug :one
SELECT * FROM tenants WHERE lower(slug) = lower($1);

-- name: GetTenantByID :one
SELECT * FROM tenants WHERE id = $1;

-- name: CreateTenant :one
INSERT INTO tenants (slug, name)
VALUES ($1, $2)
RETURNING *;

-- name: CreateOrgTenant :one
INSERT INTO tenants (slug, name, kind)
VALUES ($1, $2, 'org')
RETURNING id, slug, name, external_org_id, created_at, updated_at;

-- name: DeleteTenant :exec
DELETE FROM tenants WHERE id = $1;

-- name: SetTenantExternalOrg :exec
UPDATE tenants SET external_org_id = $2 WHERE id = $1;

-- name: ListOrgTenantsForUser :many
SELECT t.id, t.slug, t.name, m.role AS member_role
FROM tenant_members m
JOIN tenants t ON t.id = m.tenant_id
WHERE m.user_id = $1 AND t.kind = 'org'
ORDER BY t.name;
