-- name: InsertAuditLog :one
INSERT INTO audit_log (actor_user_id, action, target_type, target_id, metadata, ip_address, actor_username)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: ListAuditLogFiltered :many
SELECT *
FROM audit_log
WHERE
  ($1::text = '' OR action LIKE $1 || '%')
  AND ($2::timestamptz IS NULL OR created_at >= $2)
  AND ($3::timestamptz IS NULL OR created_at <= $3)
  AND ($6::text = '' OR actor_username = $6)
ORDER BY created_at DESC
LIMIT $4 OFFSET $5;

-- name: ListAuditLogActors :many
SELECT DISTINCT actor_username
FROM audit_log
WHERE actor_username <> ''
ORDER BY actor_username;

-- name: CountAuditLogFiltered :one
SELECT count(*)
FROM audit_log
WHERE
  ($1::text = '' OR action LIKE $1 || '%')
  AND ($2::timestamptz IS NULL OR created_at >= $2)
  AND ($3::timestamptz IS NULL OR created_at <= $3)
  AND ($4::text = '' OR actor_username = $4);
