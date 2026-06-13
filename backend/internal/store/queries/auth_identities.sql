-- name: CreateAuthIdentity :one
INSERT INTO auth_identities (user_id, provider, subject, secret_hash)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: GetAuthIdentity :one
SELECT * FROM auth_identities
WHERE provider = $1 AND subject = $2;

-- name: ListAuthIdentitiesForUser :many
SELECT * FROM auth_identities
WHERE user_id = $1
ORDER BY provider;

-- name: UpdateAuthIdentitySecret :exec
UPDATE auth_identities
SET secret_hash = $2
WHERE id = $1;
