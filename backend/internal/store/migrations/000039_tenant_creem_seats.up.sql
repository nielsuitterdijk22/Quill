-- Seat sync (2026-11-16): the seat count last confirmed on the tenant's Creem
-- subscription. 0 = not yet synced; a mismatch with the member count makes the
-- next sync (membership change, webhook, billing view) update Creem.
ALTER TABLE tenant_subscriptions ADD COLUMN creem_seats integer NOT NULL DEFAULT 0;
