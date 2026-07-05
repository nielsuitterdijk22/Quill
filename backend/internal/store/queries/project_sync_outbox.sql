-- name: InsertProjectSyncEvent :one
-- Enqueue a project-mirror event. The caller supplies the event id and
-- occurred_at explicitly so they match the values embedded in the JSON payload
-- (Tempo dedupes intake on the event id). Called inside the same transaction as
-- the project mutation.
INSERT INTO project_sync_outbox (id, project_id, event_type, payload, occurred_at)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: ListPendingProjectSyncEvents :many
-- Undelivered events that are due for a delivery attempt, oldest first.
SELECT * FROM project_sync_outbox
WHERE delivered_at IS NULL AND next_attempt_at <= now()
ORDER BY occurred_at
LIMIT $1;

-- name: MarkProjectSyncEventDelivered :exec
UPDATE project_sync_outbox
SET delivered_at = now()
WHERE id = $1;

-- name: MarkProjectSyncEventFailed :exec
-- Record a failed delivery attempt and schedule the next one.
UPDATE project_sync_outbox
SET attempts = attempts + 1, next_attempt_at = $2
WHERE id = $1;

-- name: CountProjectSyncEventsByProject :one
SELECT count(*) FROM project_sync_outbox WHERE project_id = $1;

-- name: ListProjectsWithTenant :many
-- Every project with its tenant slug, for the one-shot backfill.
SELECT p.id, p.slug, p.name, p.is_personal, t.slug AS tenant_slug
FROM projects p
JOIN tenants t ON t.id = p.tenant_id
ORDER BY p.created_at;
