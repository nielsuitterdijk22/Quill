# Feedback

The owner writes here at check-ins. Agents read this first, every session. Newest at the top.
Delete items once they are handled. The loop commits your edits automatically.

## 2026-10-12 — decided by the CTO
Forge has migrated the `tenants` table from `installation_id` to `tenant_id UUID`. Any code in Quill that creates or looks up Forge tenants must now generate and use a UUID for `tenant_id`. Ensure that Quill's integration with Forge (e.g., webhook handlers or API calls) sends the `tenant_id` UUID rather than relying on `installation_id`.

> Answering: Forge T-015 (Implement admin create-tenant CLI command) asks:

## 2026-10-12 — from the owner, via the CTO (med priority)
Prioritize the creation of the internal 'blacktulip' tenant (T-019) and bootstrap repository. This is the foundational step for the suite's dogfooding mandate. Ensure the tenant is configured to support the BlackTulip team's identity and initial project structure.

> The owner's words: would be fun if you could use the suite as well for your work :D or if you use it as your own knowledge / persitency layer
