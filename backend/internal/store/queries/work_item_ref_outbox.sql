-- name: InsertWorkItemRefEvent :one
-- Enqueue a work-item-refs push. The caller supplies the row id and occurred_at
-- explicitly so they match the values used by the dispatcher's logs and any
-- future dedupe. Called from the Forgejo webhook handler after scanning a
-- push/pull_request for work-item keys.
INSERT INTO work_item_ref_outbox (id, project_id, payload, occurred_at)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: ListPendingWorkItemRefEvents :many
-- Undelivered pushes that are due for a delivery attempt, oldest first.
SELECT * FROM work_item_ref_outbox
WHERE delivered_at IS NULL AND next_attempt_at <= now()
ORDER BY occurred_at
LIMIT $1;

-- name: MarkWorkItemRefEventDelivered :exec
UPDATE work_item_ref_outbox
SET delivered_at = now()
WHERE id = $1;

-- name: MarkWorkItemRefEventFailed :exec
-- Record a failed delivery attempt and schedule the next one.
UPDATE work_item_ref_outbox
SET attempts = attempts + 1, next_attempt_at = $2
WHERE id = $1;
