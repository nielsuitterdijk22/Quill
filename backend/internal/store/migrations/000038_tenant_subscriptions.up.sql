-- Paid teams (2026-11-16): organizations are no longer free. A new org starts
-- a 14-day Team trial (plan 'trial'); when the trial ends without a paid
-- subscription, or a subscription is canceled/expires (plan 'lapsed'), the org
-- locks: existing data stays readable, new repos/pipelines/invites are refused.
-- A side table keeps tenants (sqlc, hand-curated scans) untouched and carries
-- the Creem ids needed for seat updates and the customer portal later.
CREATE TABLE tenant_subscriptions (
    tenant_id             uuid PRIMARY KEY REFERENCES tenants(id) ON DELETE CASCADE,
    trial_ends_at         timestamptz,
    creem_customer_id     text NOT NULL DEFAULT '',
    creem_subscription_id text NOT NULL DEFAULT '',
    updated_at            timestamptz NOT NULL DEFAULT now()
);

-- Existing free orgs (no paying customers yet) get a fresh 14-day trial
-- instead of silently keeping free unlimited-membership teams.
INSERT INTO tenant_subscriptions (tenant_id, trial_ends_at)
SELECT id, now() + interval '14 days' FROM tenants WHERE kind = 'org' AND plan = 'free'
ON CONFLICT (tenant_id) DO NOTHING;
UPDATE tenants SET plan = 'trial' WHERE kind = 'org' AND plan = 'free';
