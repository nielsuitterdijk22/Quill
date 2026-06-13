-- name: CreateOrganization :one
INSERT INTO organizations (slug, name, description, parent_id)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: GetOrganizationByID :one
SELECT * FROM organizations WHERE id = $1;

-- name: GetOrganizationBySlug :one
SELECT * FROM organizations WHERE lower(slug) = lower($1);

-- name: ListOrganizations :many
SELECT * FROM organizations
ORDER BY slug
LIMIT $1 OFFSET $2;

-- name: CountOrganizations :one
SELECT count(*) FROM organizations;

-- name: SetOrganizationForgejoName :one
UPDATE organizations
SET forgejo_org_name = $2
WHERE id = $1
RETURNING *;
