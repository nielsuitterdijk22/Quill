-- Domain-ownership verification challenge for federated SSO.
--
-- Zitadel is the source of truth for whether an org's email domain is verified
-- (Quill live-reads that flag at display time), but the verification *challenge*
-- must be persisted: Zitadel rotates the token on every _generate_validation call,
-- so re-generating on each page load would invalidate the DNS record the customer
-- already added. We store the generated challenge so the same record is shown
-- across page loads.
--
-- domain_verification_type  — "dns" | "http" | "" (none generated yet)
-- domain_verification_token — the Zitadel-issued challenge token (the TXT value)

BEGIN;

ALTER TABLE tenant_sso_config
  ADD COLUMN domain_verification_type text NOT NULL DEFAULT '',
  ADD COLUMN domain_verification_token text NOT NULL DEFAULT '';

COMMIT;
