-- Work-item external-ref outbox.
--
-- Quill's Forgejo webhook receiver scans commit messages, PR titles/bodies, and
-- branch names for Tempo work-item keys ([A-Z][A-Z0-9]*-\d+) and pushes the
-- matches to Tempo's work-item-refs endpoint so a PR/commit can be cross-linked
-- to the work items it mentions. The webhook must ack fast and must not block on
-- Tempo's availability, so — exactly like the project-mirror outbox — the
-- matches are written here and a background dispatcher delivers undelivered rows
-- with retry + exponential backoff. This makes the push durable across both
-- Tempo downtime and a Quill restart mid-delivery.
--
-- Deliberately NO foreign key to projects(id): the row is a fire-and-forget
-- notification keyed by the Quill project id the pushing repo belongs to; it
-- must survive independently of the project row's lifecycle.

BEGIN;

CREATE TABLE work_item_ref_outbox (
  id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  project_id      uuid NOT NULL,
  payload         jsonb NOT NULL,
  occurred_at     timestamptz NOT NULL DEFAULT now(),
  attempts        integer NOT NULL DEFAULT 0,
  next_attempt_at timestamptz NOT NULL DEFAULT now(),
  delivered_at    timestamptz
);

-- Dispatcher poll path: undelivered rows due for a (re)delivery attempt, oldest
-- first. Partial index stays small — delivered rows drop out of it entirely.
CREATE INDEX work_item_ref_outbox_pending_idx
  ON work_item_ref_outbox (next_attempt_at)
  WHERE delivered_at IS NULL;

COMMIT;
