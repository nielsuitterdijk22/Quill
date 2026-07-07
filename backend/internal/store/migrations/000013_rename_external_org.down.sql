ALTER INDEX tenants_external_org_id_key RENAME TO tenants_clerk_org_id_key;
ALTER TABLE tenants RENAME COLUMN external_org_id TO clerk_org_id;
