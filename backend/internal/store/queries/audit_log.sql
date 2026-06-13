-- name: InsertAuditLog :one
INSERT INTO audit_log (actor_user_id, action, target_type, target_id, metadata)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: ListAuditLog :many
SELECT * FROM audit_log
ORDER BY created_at DESC
LIMIT $1 OFFSET $2;
