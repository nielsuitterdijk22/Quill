-- Organization member invitations.
--
-- An org admin invites a person by email. In a Zitadel deployment the email is
-- sent by Zitadel's mail service (the same one that sends signup verification);
-- everywhere else the admin shares the accept link. Either way this row is
-- Quill's own record of the pending invite, and accepting it adds a tenant_member.
--
-- Only a hash of the accept token is stored (like git tokens), never the raw
-- value; the raw token is shown once at creation time so it can be put in a link.

BEGIN;

CREATE TABLE org_invites (
  id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id        uuid NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  email            text NOT NULL,
  role             text NOT NULL DEFAULT 'member', -- 'admin' | 'member'
  token_hash       text NOT NULL,
  status           text NOT NULL DEFAULT 'pending', -- 'pending' | 'accepted' | 'revoked'
  invited_by       uuid REFERENCES users(id) ON DELETE SET NULL,
  accepted_user_id uuid REFERENCES users(id) ON DELETE SET NULL,
  expires_at       timestamptz NOT NULL,
  created_at       timestamptz NOT NULL DEFAULT now(),
  accepted_at      timestamptz
);

CREATE INDEX org_invites_tenant_idx ON org_invites (tenant_id);
CREATE UNIQUE INDEX org_invites_token_hash_idx ON org_invites (token_hash);

-- At most one pending invite per (org, email); a re-invite replaces the prior one.
CREATE UNIQUE INDEX org_invites_pending_email_idx
  ON org_invites (tenant_id, lower(email))
  WHERE status = 'pending';

COMMIT;
