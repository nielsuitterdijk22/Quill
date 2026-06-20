-- Add Clerk org mapping to tenants so each Clerk organisation resolves to a
-- Quill tenant on first login. The partial unique index allows NULL (tenants
-- that pre-date Clerk, e.g. the seeded default) while still enforcing
-- uniqueness for every non-NULL org ID.
ALTER TABLE tenants ADD COLUMN clerk_org_id TEXT;
CREATE UNIQUE INDEX tenants_clerk_org_id_key ON tenants (clerk_org_id) WHERE clerk_org_id IS NOT NULL;
