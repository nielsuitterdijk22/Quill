-- name: GetTenantBySlug :one
SELECT * FROM tenants WHERE lower(slug) = lower($1);

-- name: GetTenantByID :one
SELECT * FROM tenants WHERE id = $1;

-- name: CreateTenant :one
INSERT INTO tenants (slug, name)
VALUES ($1, $2)
RETURNING *;
