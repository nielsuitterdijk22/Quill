-- Freemium default: new tenants start on the 'free' plan with hard limits
-- (repos, storage, pipeline minutes) enforced at the write paths. Existing
-- tenants keep their current plan value (the seeded default tenant and any
-- operator dogfooding tenants created before this migration hold 'internal',
-- i.e. unlimited); plan changes flow through the platform-admin billing API.
ALTER TABLE tenants ALTER COLUMN plan SET DEFAULT 'free';
