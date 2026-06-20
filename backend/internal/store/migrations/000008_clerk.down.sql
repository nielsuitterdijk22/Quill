DROP INDEX IF EXISTS tenants_clerk_org_id_key;
ALTER TABLE tenants DROP COLUMN IF EXISTS clerk_org_id;
