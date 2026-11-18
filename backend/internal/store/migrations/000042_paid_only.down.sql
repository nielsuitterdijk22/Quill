DROP TABLE IF EXISTS beta_invite_redemptions;
DROP TABLE IF EXISTS beta_invites;
ALTER TABLE tenants ALTER COLUMN plan SET DEFAULT 'free';
