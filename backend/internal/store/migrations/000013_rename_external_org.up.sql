-- Clerk has been removed; the per-org tenant mapping is now driven by Zitadel.
-- Rename the column and its partial unique index to provider-neutral names.
ALTER TABLE tenants RENAME COLUMN clerk_org_id TO external_org_id;
ALTER INDEX tenants_clerk_org_id_key RENAME TO tenants_external_org_id_key;
