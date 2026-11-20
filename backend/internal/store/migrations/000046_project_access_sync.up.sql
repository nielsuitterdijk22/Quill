-- Mirror project access to Tempo.
--
-- Quill owns who can reach a project; Tempo keeps a local, always-available copy
-- (its permission checks are a hot path). Access changes therefore ride the same
-- outbox as project lifecycle events, as 'access' events. Triggers queue them so
-- no write path (API, SCIM, offboarding, group edits) can forget; the payload is
-- built when the event is delivered, from project_access(), so it is always the
-- current truth and a retry never sends a stale snapshot.
--
-- Deduplication: a burst of changes queues one event. snapshot_at records that a
-- delivery attempt has already read the data; the trigger only dedupes against an
-- event that has NOT been read yet (and locks it while checking), so a change that
-- lands while an event is in flight always queues a fresh one instead of being lost.
BEGIN;

ALTER TABLE project_sync_outbox ADD COLUMN snapshot_at timestamptz;

CREATE OR REPLACE FUNCTION enqueue_project_access_sync(p_project uuid) RETURNS void
LANGUAGE plpgsql AS $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM projects WHERE id = p_project) THEN
    RETURN; -- the project is being deleted
  END IF;
  PERFORM 1 FROM project_sync_outbox
   WHERE project_id = p_project AND event_type = 'access'
     AND delivered_at IS NULL AND snapshot_at IS NULL
   FOR UPDATE;
  IF FOUND THEN
    RETURN; -- an unread event is already queued
  END IF;
  -- clock_timestamp(), not now(): sorts after a 'create' queued earlier in the same transaction.
  INSERT INTO project_sync_outbox (project_id, event_type, payload, occurred_at)
  VALUES (p_project, 'access', '{}'::jsonb, clock_timestamp());
END $$;

CREATE OR REPLACE FUNCTION trg_project_access_sync() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
  rec record := COALESCE(NEW, OLD);
  pid uuid;
BEGIN
  IF TG_TABLE_NAME IN ('project_members', 'group_project_roles') THEN
    PERFORM enqueue_project_access_sync(rec.project_id);
  ELSIF TG_TABLE_NAME = 'group_members' THEN
    FOR pid IN SELECT project_id FROM group_project_roles WHERE group_id = rec.group_id LOOP
      PERFORM enqueue_project_access_sync(pid);
    END LOOP;
  ELSIF TG_TABLE_NAME = 'tenant_members' THEN
    FOR pid IN SELECT id FROM projects WHERE tenant_id = rec.tenant_id LOOP
      PERFORM enqueue_project_access_sync(pid);
    END LOOP;
  END IF;
  RETURN NULL;
END $$;

CREATE TRIGGER project_members_access_sync AFTER INSERT OR UPDATE OR DELETE ON project_members
  FOR EACH ROW EXECUTE FUNCTION trg_project_access_sync();
CREATE TRIGGER group_project_roles_access_sync AFTER INSERT OR UPDATE OR DELETE ON group_project_roles
  FOR EACH ROW EXECUTE FUNCTION trg_project_access_sync();
CREATE TRIGGER group_members_access_sync AFTER INSERT OR UPDATE OR DELETE ON group_members
  FOR EACH ROW EXECUTE FUNCTION trg_project_access_sync();
CREATE TRIGGER tenant_members_access_sync AFTER INSERT OR UPDATE OR DELETE ON tenant_members
  FOR EACH ROW EXECUTE FUNCTION trg_project_access_sync();

-- Every existing project gets one access event, so Tempo learns today's access.
INSERT INTO project_sync_outbox (project_id, event_type, payload, occurred_at)
SELECT id, 'access', '{}'::jsonb, clock_timestamp() FROM projects;

COMMIT;
