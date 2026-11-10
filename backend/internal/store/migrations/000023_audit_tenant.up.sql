-- Tenant-scoped audit log: every entry records the actor's tenant at write
-- time (NULL for system actions and legacy accounts), so reads can be
-- strictly isolated per tenant - no admin sees another tenant's entries.

BEGIN;

ALTER TABLE audit_log ADD COLUMN tenant_id uuid;

CREATE INDEX audit_log_tenant_idx ON audit_log (tenant_id);

-- Backfill history from the acting user's account tenant.
UPDATE audit_log a
SET tenant_id = u.tenant_id
FROM users u
WHERE u.id = a.actor_user_id AND u.tenant_id IS NOT NULL;

COMMIT;
