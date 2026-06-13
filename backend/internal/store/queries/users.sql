-- name: CreateUser :one
INSERT INTO users (username, email, display_name, is_admin, is_active)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetUserByID :one
SELECT * FROM users WHERE id = $1;

-- name: GetUserByUsername :one
SELECT * FROM users WHERE lower(username) = lower($1);

-- name: GetUserByEmail :one
SELECT * FROM users WHERE lower(email) = lower($1);

-- name: ListUsers :many
SELECT * FROM users
ORDER BY username
LIMIT $1 OFFSET $2;

-- name: CountUsers :one
SELECT count(*) FROM users;

-- name: SetUserForgejoLink :one
UPDATE users
SET forgejo_user_id = $2, forgejo_username = $3
WHERE id = $1
RETURNING *;
