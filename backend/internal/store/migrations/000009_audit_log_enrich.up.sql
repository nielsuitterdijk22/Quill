BEGIN;

ALTER TABLE audit_log
  ADD COLUMN ip_address     text NOT NULL DEFAULT '',
  ADD COLUMN actor_username text NOT NULL DEFAULT '';

CREATE INDEX audit_log_action_idx ON audit_log (action);

COMMIT;
