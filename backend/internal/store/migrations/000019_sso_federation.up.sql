-- Federated SSO consumption support.
--
-- external_idp_id records the Zitadel identity-provider id created for an org's
-- SSO config, so a later edit or disable can update/remove exactly that IdP
-- rather than guessing. It is empty until the config is first provisioned into
-- Zitadel.
--
-- username_confirmed marks whether a user has chosen/confirmed their handle
-- (rather than one derived from an external IdP profile). Federated first logins
-- start false and are routed to the onboarding username step; locally-registered
-- users typed their handle, so they start true. Existing accounts are already
-- past onboarding, so they are backfilled to true.

BEGIN;

ALTER TABLE tenant_sso_config
  ADD COLUMN external_idp_id text NOT NULL DEFAULT '';

ALTER TABLE users
  ADD COLUMN username_confirmed boolean NOT NULL DEFAULT false;

UPDATE users SET username_confirmed = true;

COMMIT;
