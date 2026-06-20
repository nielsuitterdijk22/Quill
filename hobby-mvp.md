# Hobby project sharing — MVP checklist

Goal: get Quill to a state where people (especially Europeans) can self-host it
for personal and hobby projects as a credible GitHub alternative. Scope: one
instance, multiple users, repos, PRs, basic CI. No orgs, no enterprise, no
OIDC — that comes later.

---

## 🔴 Blockers — must land before sharing

### Security

- [x] **Rate-limit auth endpoints.** `/auth/register` and `/auth/login` have no
  brute-force protection. Add a per-IP sliding-window limiter (e.g. 10
  attempts / minute) in the chi middleware stack.
- [x] **Harden JWT cookie flags.** Audit the cookie set in the login handler:
  confirm `HttpOnly`, `Secure` (flag gated on `QUILL_ENV=production`), and
  `SameSite=Strict` are all set. A missing `Secure` flag means the session
  cookie travels over plain HTTP.
- [x] **Security response headers.** Add a middleware pass in `server/` that sets
  `Content-Security-Policy`, `X-Frame-Options: DENY`,
  `X-Content-Type-Options: nosniff`, and `Referrer-Policy: no-referrer` on
  every response.
- [x] **Path traversal check on `/contents`.** The content endpoint passes a
  user-supplied path to Forgejo — confirm Forgejo itself sanitises this and
  that the Quill handler never constructs filesystem paths from it.
- [x] **Document HTTPS requirement.** The deploy guide must warn that running
  Quill over plain HTTP in production leaks session cookies. Add a
  reverse-proxy example (Caddy or nginx) with TLS to `deploy/compose/`.

### Correctness

- [x] **Pipeline status in PR merge gate.** Branch policies (PR 7) block merge;
  pipeline checks do not yet participate. Wire the pipeline run result for the
  head commit into the merge-gate evaluation so a failing CI job can block
  merge — with a per-repo toggle to make it required vs informational.
- [x] **`viewerIsAuthor` flag from the backend.** The PR page currently compares
  the Quill username to `pull.author.login` in the frontend. That breaks once
  Forgejo accounts are provisioned under a different login (OIDC future, or
  even an admin rename). Add an explicit boolean to the PR response object and
  use it everywhere the frontend makes self/other decisions.
- [ ] **Git token orphan on double failure.** If minting a token succeeds in
  Forgejo but the DB write fails, and the compensating Forgejo delete also
  fails, a `quill-git-*` token is silently orphaned. Add a reconcile path:
  on list-tokens, compare Forgejo tokens (filtered by `quill-git-` prefix)
  against DB records and surface any that have no matching row.

### Stability

- [x] **Graceful shutdown in `cmd/api` and `cmd/dispatch`.** Both entrypoints
  should catch `SIGTERM`/`SIGINT`, stop accepting new requests, drain in-
  flight requests (with a timeout), and close the DB pool cleanly. Required
  for container restarts without dropped requests.
- [x] **Health check endpoints.** Add `GET /healthz` that checks Postgres
  connectivity and (optionally) Forgejo reachability, returning `200` or
  `503`. Needed for Docker Compose `healthcheck:`, load balancers, and
  uptime monitors.
- [x] **Forgejo unavailable handling.** If Forgejo is down, most routes return an
  opaque 500. Add a sentinel error type for Forgejo unreachable and return a
  clear `503 Service Unavailable` with a user-facing message instead of a
  blank screen or panic.

---

## 🟠 Core UX — first impression

### Onboarding

- [x] **Landing page for logged-out visitors.** Right now an unauthenticated
  visitor hitting `/` probably gets a redirect to login. Add a minimal landing
  page that explains what Quill is, who it's for, and has a Register CTA.
  Two paragraphs and a button is enough.
- [x] **Empty state for Projects page.** When a new user has no projects, show a
  prompt ("Create your first project") with a direct link to the creation
  form, not a blank list.
- [x] **Empty states for Repos, PRs, and Pipelines.** Each list page should have
  contextual empty-state copy rather than rendering an empty table.
- [x] **Post-registration flow.** After registering, drop the user directly into
  project creation (or a "get started" checklist) rather than the empty
  dashboard.

### User account basics

- [x] **Password change.** Add a "Change password" section to the settings page
  (current password + new password + confirm).
- [x] **Forgot password / reset flow.** Minimum viable: admin-reset via a CLI
  command (`quill admin reset-password <username>`) so self-hosters don't
  lose access. Full email-based reset if SMTP is configured.
- [x] **Account deletion (GDPR — see below).** Self-service "Delete my account"
  in settings: purges the Quill DB record, revokes all git tokens, and
  deletes the mirrored Forgejo account.
- [x] **Email change.** Let users update their email address (settings page).

### Repository basics

- [x] **Clone URL displayed prominently.** On the repo overview page, show the
  HTTP clone URL (and SSH if wired) in a copyable `<input>` at the top, like
  GitHub does. Currently it's unclear how to clone.
- [x] **SSH key management.** Let users add their public SSH keys (stored in
  Forgejo via the admin client). This removes the need to create a git token
  just to `git clone`.
- [x] **Public / private visibility toggle.** Confirm the repo visibility setting
  surfaces in the UI and actually makes the repo readable without
  authentication via Forgejo. Hobby users will want to share public repos.
- [x] **Repo description and website field.** Forgejo stores these; display them
  on the repo overview page and let users edit them from repo settings.

### Error handling

- [x] **404 pages for missing resources.** Navigating to a non-existent project,
  repo, or PR should show a clear 404, not a crash or infinite spinner.
- [x] **API error propagation.** When backend calls fail, the frontend should
  display the error message (or a friendly fallback) rather than silently
  failing or showing a blank component.

### README display

- [x] **Cache `/markup` renders.** Every repo tree page does a POST to Forgejo
  `/markup` on load. Add a short-lived in-process cache keyed on
  `(repo, content-SHA)` in the API handler to avoid repeated round-trips on
  back/forward navigation.
- [x] **Auto-detect README.** If the repo root contains a `README.md` (or
  `.rst`, `.txt`), render it below the file tree on the repo overview page
  automatically.

---

## 🟡 European / privacy appeal

### GDPR basics

- [x] **Account deletion (full purge).** Covered in UX above — emphasising it
  here because it is a legal requirement for European deployments. The delete
  path must: remove the `users` + `auth_identities` rows, revoke all
  `git_tokens`, call the Forgejo admin API to delete the mirrored account,
  and remove the user from all `project_members` rows. Log what was deleted
  for the operator's audit trail.
- [x] **Data export.** Let users download a ZIP or JSON of their own data:
  profile info, repos they own, PRs they authored, and pipeline runs they
  triggered. A one-endpoint export (`GET /me/export`) is sufficient for
  GDPR Article 20 portability; it does not need to be pretty.
- [x] **No external resources in the frontend.** Audit `frontend/` for any
  Google Fonts `<link>`, CDN `<script>` tags, or analytics pixels. Self-
  hosters should serve all assets from their own domain so no third-party
  receives user IP addresses. Bundle any fonts locally.
- [x] **Cookie consent banner not required (document this).** If Quill uses only
  the session JWT cookie and no analytics, GDPR's cookie consent requirement
  does not apply. State this explicitly in the deploy guide and confirm no
  `localStorage` analytics are written.
- [x] **Privacy policy template.** Add a `docs/operator-privacy-policy.md`
  template that instance operators can adapt. It should cover: what data
  Quill stores, retention, user rights (access, rectification, erasure,
  portability), and contact details for the DPO/operator.

### Self-hosting quality

- [ ] **End-to-end smoke test of `docker compose up`.** From a completely clean
  checkout, run `docker compose up` in `deploy/compose/` and verify that
  registration, project creation, repo push, PR creation, and pipeline
  trigger all work without any manual steps. Fix whatever breaks.
- [x] **Document all environment variables.** Every variable in `.env.example`
  must have a one-line comment explaining what it does and what the default
  is. Add any that are currently undocumented (e.g. `QUILL_JWT_TTL`,
  `QUILL_JWT_SECRET`, `FORGEJO_ADMIN_PASSWORD`, SMTP vars).
- [x] **Automatic first-boot provisioning.** The Forgejo admin account and the
  Quill admin user must be created reliably on first boot without manual
  intervention. Add a startup probe or init container that retries until
  Forgejo is ready before the API starts.
- [x] **Backup and restore documentation.** Document how to dump Postgres
  (`pg_dump`) and back up the Forgejo data volume, and how to restore from
  both. One page in `deploy/compose/README.md` is enough.
- [x] **Upgrade path documentation.** When a new Quill release ships a schema
  migration, operators need to know to run `make migrate` (or whatever the
  command is) before starting the new API. Document this prominently.
- [x] **Resource requirements.** State the minimum RAM and CPU for a hobby
  instance (Forgejo + Postgres + Quill API + dispatcher). Rough guide so
  users know if a €5/month VPS is enough.

### Transparency

- [x] **License and source in UI footer.** The footer (or about page) should
  link to the GitHub repo and display "Apache 2.0" — so users of a hosted
  instance know their rights.
- [x] **No telemetry statement in README.** Add a one-liner confirming Quill
  makes no call-home requests and collects no usage telemetry by default.
  This is a differentiator from GitHub and a selling point for Europeans.

---

## 🔵 Finish PR 8 — pipelines

- [ ] **Log streaming.** Replace the one-shot log return (logs returned only on
  run completion) with a streaming approach: Server-Sent Events from the
  dispatcher so users see live output. The `pipeline.Runner` interface
  already abstracts this; the act runner just needs to emit chunks.
- [x] **Pipeline status badges on PR page.** Show a ✓ / ✗ / ⏳ check summary
  on the PR conversation view for all runs associated with the head commit,
  mirroring GitHub's Checks section.
- [ ] **Pipeline required-checks toggle.** Add a per-repo setting (in branch
  policies or a separate "required checks" config) listing which workflow
  names must pass before merge is allowed.
- [x] **Re-run button.** On the pipeline run detail page, add a "Re-run" button
  that triggers a fresh run of the same workflow at the same ref.
- [ ] **Run cancellation.** If a run is in progress, let the user cancel it (kill
  the act process, mark the run as cancelled in the DB).
- [x] **Run timeout.** Enforce a per-run wall-clock timeout (configurable via env
  var, default 30 min) to prevent stuck Docker containers.
- [ ] **Rootless runner option.** The current runner mounts the Docker socket,
  which gives pipeline jobs root on the host. Investigate Docker-in-Docker or
  Podman as a rootless alternative and document the trade-offs for self-
  hosters who don't want to expose the socket.
- [x] **Runner error surface.** If the Docker daemon is unreachable when a run
  is triggered, the user should see a clear "Runner unavailable" error on the
  run detail page rather than a perpetually pending run.

---

## 🟢 Nice to have — post-sharing

### Notifications

- [ ] In-app notification bell: PR reviews, CI failures, mentions in comments.
- [ ] Email notifications (SMTP optional): PR assigned, CI failed, PR merged.
  Gate on `QUILL_SMTP_*` env vars being set.

### Issues

- [ ] Expose Forgejo's issue tracker through the Quill UI (list, create, comment,
  close). Forgejo already stores issues; Quill just needs routes and pages.
- [ ] `closes #N` in a commit message or PR description auto-closes the linked
  issue on merge (Forgejo already handles this if the PR description is
  written to Forgejo; verify it surfaces correctly).

### Git / repo features

- [ ] **Repo forking UI.** Forgejo supports fork; expose it in the Quill UI so
  users can fork a public repo into their own project.
- [ ] **Git LFS.** Confirm whether Forgejo's LFS works end-to-end through the
  Quill git token auth flow. Document it if it does; note the limitation if
  it doesn't.
- [ ] **Repo starring / watching.** Basic social signal; low priority but helps
  discoverability on a shared instance.

### Admin panel

- [ ] List users, disable/enable accounts, admin-reset passwords.
- [ ] View all repos, pipeline run counts, and rough storage usage.
- [ ] Per-user pipeline rate limiting (prevent one user from queuing 50
  concurrent runs).

### Token metadata

- [ ] Surface token scope and last-used timestamp (Forgejo exposes these) in the
  git tokens list so users can audit which tokens are still active.
- [ ] Warn on duplicate token display names (only the Forgejo-side name is
  unique today).

### Polish

- [ ] Keyboard shortcuts (j/k to navigate PR list, ? for help overlay).
- [ ] Light / dark mode toggle (currently appears dark-only).
- [ ] Mobile-responsive pass on the most common pages (repo tree, PR view).
- [ ] Favicon and consistent branding (name in tab title, og:image for link
  previews).

---

## 📋 Process checklist

- [x] Write `SELF_HOSTING.md` (or expand `deploy/compose/README.md`) covering:
  minimum requirements, first-boot steps, HTTPS setup, backup procedure,
  upgrade procedure, GDPR operator checklist.
- [ ] Add end-to-end integration tests for the critical happy path: register →
  create project → push repo → open PR → CI runs → merge. Gate on
  `QUILL_TEST_DATABASE_URL` like the existing integration tests.
- [x] Write a "why Quill?" section in `README.md` — two paragraphs covering
  self-hosting, data sovereignty, and no vendor lock-in. Speak directly to
  the European developer who wants off GitHub.
- [ ] Set up a public demo instance (optional but accelerates adoption — link
  from README with a note that data is wiped periodically).
