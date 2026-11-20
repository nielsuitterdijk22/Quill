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

-- name: MarkProjectSyncEventSnapshot :exec
-- Record that a delivery attempt has read the data for an 'access' event, so the
-- triggers queue a fresh event for any later change instead of deduping into it.
UPDATE project_sync_outbox SET snapshot_at = now() WHERE id = $1;

-- name: CountProjectLifecycleEvents :one
-- Lifecycle events (create/rename/archive/delete) only; 'access' events do not
-- count, so the create backfill still reaches projects that predate the mirror.
SELECT count(*) FROM project_sync_outbox WHERE project_id = $1 AND event_type <> 'access';

-- name: ListProjectAccessSnapshot :many
-- ListProjectAccessSnapshot is everything Tempo needs to mirror who can reach a
-- project: the tenant roster (with the tenant role) and each person's resolved
-- role from project_access(), the same single definition Quill authorizes with.
-- role is '' for a roster member with no access. Org admins have no standing
-- access, so they appear on the roster with role ''. Personal tenants have no
-- roster; their roster is the project's own members.
SELECT u.id AS user_id, u.username, u.email, u.display_name, r.tenant_role,
       COALESCE((SELECT a.role FROM project_access(u.id, sqlc.arg(project_id)::uuid) a LIMIT 1), '')::text AS role
FROM (
  SELECT tm.user_id, CASE tm.role WHEN 'admin' THEN 'owner' ELSE 'member' END AS tenant_role
  FROM projects p
  JOIN tenants t ON t.id = p.tenant_id AND t.kind = 'org'
  JOIN tenant_members tm ON tm.tenant_id = t.id
  WHERE p.id = sqlc.arg(project_id)::uuid
  UNION ALL
  SELECT pm.user_id, 'owner'
  FROM projects p
  JOIN tenants t ON t.id = p.tenant_id AND t.kind <> 'org'
  JOIN project_members pm ON pm.project_id = p.id
  WHERE p.id = sqlc.arg(project_id)::uuid
) r
JOIN users u ON u.id = r.user_id AND u.is_active
ORDER BY u.username;
