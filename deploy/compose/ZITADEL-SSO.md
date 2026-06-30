# Setting up an SSO tenant (Zitadel)

In Quill's model **one Zitadel organization = one Quill tenant**. "SSO" means that
org federates login to the customer's identity provider (Google, Microsoft/Entra,
Okta, or any OIDC/SAML IdP). Quill maps the user's org claim
(`urn:zitadel:iam:user:resourceowner:id`) to a tenant automatically on first
login, so there's nothing to configure on the Quill side — it's all done in the
**Zitadel console**.

This guide is for the platform operator (or a customer admin) configuring the
first SSO-backed company tenant.

## 1. Create the organization (the tenant)

Console → the org switcher (top-left) → **Create Organization** → name it (e.g.
`Acme`). The first time a user from this org logs in, Quill provisions them and
creates the matching tenant automatically.

## 2. Add and verify the org's domain

Open the org → **Domains** → add the company domain (e.g. `acme.com`) and verify
it (DNS TXT). This enables **domain discovery**: users who enter an `@acme.com`
address are routed to this org's SSO instead of a password prompt.

## 3. Add the Identity Provider

Org → **Identity Providers** → **New**, then pick a template:

- **Google** — easiest to test with; you create a Google OAuth client and paste
  its client id/secret.
- **Microsoft / Entra ID**, **GitHub**, **GitLab**, **Apple** — built-in templates.
- **Generic OIDC** — issuer URL + client id/secret.
- **SAML** — upload the IdP metadata XML (or URL).

Zitadel shows a **callback / ACS URL** — register that in the customer's IdP as
the allowed redirect/reply URL. Typical values:

- OIDC redirect: `https://auth.<your-domain>/ui/login/login/externalidp/callback`
- SAML ACS: `https://auth.<your-domain>/ui/login/login/externalidp/saml/acs`

(The console shows the exact URLs for your instance — use those.)

## 4. Turn it on in the org's login behaviour

Org → **Login Behavior / Security** (login policy):

- Enable the IdP you added so it appears on the login screen.
- Enable **"Automatically create users"** (auto-register) so first-time SSO users
  self-provision — they then flow straight into Quill.
- Optionally enable **"Auto-redirect"** so `@acme.com` users skip the chooser and
  go straight to their IdP.

## 5. Test the round trip

A user from the org signs in → bounced to the external IdP → back through Zitadel
→ into Quill. On that first login the Quill backend:

1. verifies the Zitadel token,
2. reads the org claim and resolves/creates the org's **tenant**,
3. provisions the Quill user (and a Forgejo account).

Tenant-wide policies you set under **Policies** then apply to everyone in that org.

## Notes & gotchas

- **Email/SMTP**: invites and email verification need a working SMTP config
  (Console → Settings → SMTP). On Scaleway use a relay port that isn't blocked —
  e.g. Brevo on **2525** (25/465/587 are blocked outbound). See the SMTP section
  of `ZITADEL-GO-LIVE.md`.
- **Testing without a corporate IdP**: use Google as the IdP — most people have a
  Google account, and it proves the full federation flow end to end.
- **SCIM (directory sync)** is a separate, later step: Zitadel exposes a SCIM v2
  endpoint so the customer's directory can push users/groups in, rather than
  just-in-time provisioning on first login.
- This is all driven from the Zitadel console today; a Quill-native org/SSO admin
  UI (create org + members + IdP via the Management API) is planned but not
  required — the console is fully featured.
