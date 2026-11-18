-- Paid only (2026-11-18): no free tier, no trial. New accounts start 'inactive'
-- (locked until they subscribe or redeem a hand-issued beta invite); the
-- individual paid plans are 'starter' and 'pro', organizations are 'team'.
-- 'beta' is granted free by the vendor (invite link below, or the admin plan
-- switch). The old free/trial values no longer exist: those tenants lock.
ALTER TABLE tenants ALTER COLUMN plan SET DEFAULT 'inactive';
UPDATE tenants SET plan = 'inactive', updated_at = now() WHERE plan IN ('free', 'trial');

-- Beta invites: temporary, capped links the owner hands to hand-picked users.
-- Only the SHA-256 of the code is stored (the raw code is shown once).
CREATE TABLE beta_invites (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    code_hash   text NOT NULL UNIQUE,
    note        text NOT NULL DEFAULT '',
    max_uses    integer NOT NULL DEFAULT 1 CHECK (max_uses > 0),
    uses        integer NOT NULL DEFAULT 0,
    expires_at  timestamptz NOT NULL,
    revoked_at  timestamptz,
    created_by  uuid REFERENCES users(id) ON DELETE SET NULL,
    created_at  timestamptz NOT NULL DEFAULT now()
);

-- Which account redeemed which invite (audit trail; one row per account).
CREATE TABLE beta_invite_redemptions (
    tenant_id   uuid PRIMARY KEY REFERENCES tenants(id) ON DELETE CASCADE,
    invite_id   uuid NOT NULL REFERENCES beta_invites(id) ON DELETE CASCADE,
    redeemed_by uuid REFERENCES users(id) ON DELETE SET NULL,
    redeemed_at timestamptz NOT NULL DEFAULT now()
);
