# Suite

A sovereign, EU-hosted engineering suite that replaces the GitHub + Azure DevOps + CI + Backstage stack with one integrated product, sold at a low per-seat price with usage-based overage, so mid-size European orgs get their tooling back from US hyperscalers without giving up simplicity. It feels like one product because every screen cross-links the others in real time.

> Owner document, shared by every product in the suite. Agents read it before MISSION.md,
> treat it as read-only, and follow its shared decisions where a product's documents are silent
> or disagree.

## Problem
Mid-size EU orgs (200-2000 engineers) are locked into US-hosted GitHub, Azure DevOps, and CI, and want sovereignty plus a simpler, integrated toolchain without the 1001 integrations of a big-enterprise private deployment.

## Customers and users
Buyer: VP Eng / Platform lead at a 200-2000 engineer EU org, motivated by sovereignty and simplicity. Daily user: the engineer, who plans in Tempo, codes in Quill, runs CI in Forge, and discovers services in Atlas from one signed-in session. Free individual tier (50 repos, 1GB, 100 min) is the top-of-funnel wedge.

## Products
- **Quill**: Version control and code review platform on Forgejo; the home base where PRs, repos, and branch policies live and where cross-product data converges.
- **Tempo**: Agile work tracking (boards, sprints, roadmaps) with a first-class Azure DevOps importer; the planning hub that links tickets to PRs and runs.
- **Forge**: Secure ephemeral CI runners with locked-down egress and signed provenance; executes the pipelines Quill defines.
- **Atlas**: Self-service developer portal (Backstage-style) for ownership, discovery, and templated provisioning; the onboarding and who-owns-what layer.

## Priorities
- Quill + Tempo + Forge working together as the engineer's daily loop (plan, code, CI) with live cross-product linking
- Atlas as the onboarding and discovery layer, second
- Shared identity and single sign-on across all four
- Stripe billing: free individual, 5 EUR/seat team, 15 EUR/seat support tier, metered overage
- EU-hosted SaaS on a single shared multi-tenant instance

## Shared platform decisions
- One Zitadel OIDC identity for all four products; a user signs in once and is the same person everywhere
- Shared event bus (NATS) so PR, ticket, run, and service events propagate in real time for native cross-product views; each product keeps its own Postgres
- One shared dark design system (purple accent, sidebar shell) so all four feel like a single product
- Single shared multi-tenant instance, Docker Compose on one EU VPS for v1; tenant_id is the isolation boundary
- Stripe handles all billing; usage metering (storage, pipeline minutes, runner pool) feeds overage and enterprise invoicing

## Core workflows
- Engineer opens a PR in Quill and sees the linked Tempo ticket with live status, the Forge run with logs, and the Atlas service owner on the same page
- Tempo ticket shows attached PRs and their pipeline status; merging a PR auto-transitions the ticket
- New engineer uses Atlas to self-serve provision a service, which creates the Quill project/repo and Tempo project in one flow
- Platform admin grants access via Quill admin UI or an external ITSM tool calling the permission API

## Examples
- A 50-engineer fintech migrates off Azure DevOps using Tempo's importer, points Quill at their repos, and runs CI on Forge, all under one EU-hosted login
- A free individual developer self-signs up, gets 50 repos, 1GB, 100 min, and hits the upgrade prompt when they exceed it
- A bank's platform team (the harder, later sell) gets SSO/SCIM, audit log, and a named support contact on the 15 EUR tier

## Done looks like (first sellable version)
- One sign-on gets a user into all four products and they are the same identity everywhere
- A PR page shows linked ticket, pipeline run with logs, and service owner without leaving Quill
- A Tempo ticket shows its PRs and pipeline status and auto-closes on merge
- A new customer can provision a service via Atlas that creates the matching Quill and Tempo objects
- Stripe charges 5 EUR/seat, meters overage, and a free individual account exists with hard quotas
- The whole suite runs on one Docker Compose stack on an EU VPS

## Out of scope
- Self-hosted or on-prem offering (we are hosted SaaS only)
- Big-enterprise private deployment and its 1001 custom integrations in v1
- Per-work-item ACLs and complex permission sprawl
- Non-EU hosting or data residency outside the EU

## Must never happen
- Never offer a self-hosted or on-prem build; sovereignty is delivered by our EU hosting, not a tarball
- Never fork Forgejo; it is wrapped via REST
- Never add per-work-item ACLs in Tempo; permissions stay team-derived
- Never let a product call another's database directly; only via the event bus or REST API

## Constraints
- v1 is a single shared multi-tenant instance on Docker Compose on the owner's device/VPS; cloud provider migration comes after the suite is proven
- No contractual data-residency SLA in v1; pinned to a specific EU region once we move to a cloud provider
- Each product is its own repository with its own Go backend and Next.js frontend, sharing only the design system, identity, and event bus
- Pricing is 5 EUR/seat flat + metered overage + 15 EUR/seat support tier; enterprises end on custom usage-based contracts with ~50% margin

## Owner notes

- 2026-10-09: Standardize cross-product communication: REST calls must include tenant context via the `X-Tenant-Id` HTTP header. NATS events must include `tenant_id` in the JSON payload AND the `X-Tenant-Id` NATS header. This ensures tenant isolation is enforced at both the transport and application layers.
