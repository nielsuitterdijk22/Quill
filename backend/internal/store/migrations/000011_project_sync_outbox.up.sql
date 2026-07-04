-- Project-mirror outbox.
--
-- Quill owns Project identity (id, slug, name, tenant). Tempo keeps a local,
-- always-available mirror so its hot path (backlog filters, board grouping,
-- permission checks) never needs a live call into Quill. This table is the
-- transactional outbox that makes that push reliable: a row is inserted in the
-- SAME transaction as the project mutation, so an event is emitted if and only
-- if the mutation committed. A background dispatcher then delivers undelivered
-- rows to Tempo's intake endpoint with retry + exponential backoff, so events
-- survive Tempo downtime.
--
-- Deliberately NO foreign key to projects(id): a 'delete' event must outlive the
-- project row it describes (the row is deleted in the same transaction that
-- enqueues the delete event).

BEGIN;

CREATE TABLE project_sync_outbox (
  id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  project_id      uuid NOT NULL,
  event_type      text NOT NULL, -- 'create' | 'rename' | 'archive' | 'delete'
  payload         jsonb NOT NULL,
  occurred_at     timestamptz NOT NULL DEFAULT now(),
  attempts        integer NOT NULL DEFAULT 0,
  next_attempt_at timestamptz NOT NULL DEFAULT now(),
  delivered_at    timestamptz
);

-- Dispatcher poll path: undelivered rows that are due for a (re)delivery
-- attempt, oldest occurrence first. Partial index keeps it small — delivered
-- rows drop out of the index entirely.
CREATE INDEX project_sync_outbox_pending_idx
  ON project_sync_outbox (next_attempt_at)
  WHERE delivered_at IS NULL;

-- Backfill idempotency + per-project lookups: the one-shot backfill skips a
-- project that already has any outbox event, so replaying it never
-- double-enqueues a create.
CREATE INDEX project_sync_outbox_project_idx
  ON project_sync_outbox (project_id);

COMMIT;
