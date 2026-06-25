BEGIN;

DROP INDEX IF EXISTS audit_log_action_idx;

ALTER TABLE audit_log
  DROP COLUMN IF EXISTS ip_address,
  DROP COLUMN IF EXISTS actor_username;

COMMIT;
