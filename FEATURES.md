# Features

> Owner document. Agents treat it as read-only.

## F1: Unified Authentication & Identity (must) — exists

Provides secure access via local email/password for individuals and OIDC/SSO (Entra) for enterprises. Supports SCIM provisioning for automated user and group management. Ensures strict session security with HttpOnly, Secure, and SameSite=Strict cookies.

## F2: Nested Group Hierarchy & Permissions (must) — partial

Organizes repositories within a tenant using a nested group structure (max depth 5) to mirror enterprise organizational charts. Enables granular access control where permissions inherit through the hierarchy. Allows admins to grant specific roles (e.g., reader, writer) to users or groups at any level.

## F3: Pull Request Management & Merge Gates (must) — exists

Facilitates code review through pull requests with diff viewing and inline comments. Enforces branch policies that require a specific number of approvals and successful CI status before merging. Integrates with Tempo to automatically transition linked tickets to 'Done' upon merge.

## F4: Ephemeral CI Pipelines (must) — partial

Executes user-defined shell steps in isolated, ephemeral containers via the Forge integration. Displays real-time pipeline logs and status directly on the pull request page. Supports sequential step execution for build and test workflows without complex DAGs.

## F5: Usage-Based Billing & Quotas (must) — planned

Manages Stripe-based billing for storage, repository count, and pipeline minutes. Enforces fixed limits for the free individual tier (50 repos, 1GB, 100 min/mo) and usage-based invoicing for enterprises. Provides a dashboard for admins to monitor consumption and manage invoices.

## F6: Repository & Code Browsing (must) — exists

Allows developers to browse file trees, view file contents, and inspect commit history and branches. Displays repository metadata including description, website, and visibility settings. Provides prominent clone URLs for HTTPS and SSH access.

## F7: Audit Logging & Compliance (should) — planned

Records every significant action including permission changes, merges, and billing events with actor and timestamp details. Ensures tenant isolation by filtering all data queries by tenant_id to prevent cross-tenant leakage. Supports GDPR compliance through account deletion workflows that purge user data.

## F8: Notifications & Webhooks (should) — planned

Sends email notifications for key events such as PR review requests, CI failures, and @-mentions. Allows users to configure outgoing webhooks to external endpoints (e.g., Slack, ITSM) for PR lifecycle events. Enables integration with external tools via a REST API for access management.

## F9: Landing & Pricing Pages (should) — exists

Provides a public marketing site for logged-out visitors to understand the product value proposition. Displays pricing tiers for individual and enterprise plans. Serves as the entry point for self-service registration for the free tier.
