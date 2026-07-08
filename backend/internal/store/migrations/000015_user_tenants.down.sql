-- Drop the per-account tenant link. This intentionally does not reverse the data
-- backfill: the per-user tenants created by the up migration and the project
-- reassignments are left in place (harmless without the column, and reversing
-- them cannot be done unambiguously). Removing the column restores the previous
-- schema.

BEGIN;

ALTER TABLE users DROP COLUMN IF EXISTS tenant_id;

COMMIT;
