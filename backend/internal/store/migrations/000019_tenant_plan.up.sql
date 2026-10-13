-- Billing plan for a tenant.
--
-- The mission splits the product into a free individual tier and a
-- usage-based enterprise tier, but the tenants table only carried identity
-- (kind, external_org_id). This adds the plan the vendor provisions per
-- tenant; 'internal' is the safe default for everything already seeded
-- (personal tenants and the 'default' tenant) until the vendor assigns
-- billing plans.
BEGIN;

ALTER TABLE tenants ADD COLUMN plan TEXT NOT NULL DEFAULT 'internal';

COMMIT;
