-- name: CreateGitToken :one
INSERT INTO git_tokens (user_id, name, forgejo_token_name)
VALUES ($1, $2, $3)
RETURNING *;

-- name: ListGitTokensByUser :many
SELECT * FROM git_tokens
WHERE user_id = $1
ORDER BY created_at DESC;

-- name: GetGitToken :one
SELECT * FROM git_tokens
WHERE id = $1 AND user_id = $2;

-- name: DeleteGitToken :exec
DELETE FROM git_tokens
WHERE id = $1 AND user_id = $2;
