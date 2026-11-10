BEGIN;

DROP INDEX audit_log_tenant_idx;
ALTER TABLE audit_log DROP COLUMN tenant_id;

COMMIT;
