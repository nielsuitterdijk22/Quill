# Repository guidance for coding agents

Quill is a VCS platform (Go backend + Next.js frontend) layered on Forgejo. This
file orients automated contributors. Keep changes small, typed, and tested.

## Golden rules

- **Forgejo is wrapped, never forked.** Git/repo/PR primitives go through Forgejo's
  REST API (`internal/forgejo`, added in PR 4). Quill's Postgres holds only the
  platform metadata Forgejo can't: tenants, projects, ownership, branch policies,
  pipelines, and auth identity mapping.
- **Auth stays behind an interface.** Never call a specific provider directly;
  go through `AuthProvider` (PR 3) so OIDC providers drop in later.
- **Flat MVP model: Tenant → Project → Resource.** A Tenant is the billing/SSO
  boundary; a Project is a team/app namespace that owns repositories and
  pipelines directly (no teams layer). Each project maps 1:1 to a Forgejo org.
  Cross-cutting views filter by the current project (sidebar switcher, cookie
  `quill_current_project`).
- **One shared design system.** Style with the classes in
  `frontend/app/globals.css` (ported from Forge). Don't add Tailwind or inline
  ad-hoc styles; extend the system with new classes instead.

## Backend (`backend/`)

- Module: `github.com/nielsuitterdijk22/quill`. Go 1.24, stdlib `log/slog`.
- Stack: `chi` router, `pgx` + `sqlc` (typed queries), `golang-migrate`.
- Layout: `cmd/api` entrypoint; `internal/{config,logging,server,httpx,...}`.
- Before pushing: `make be-fmt be-vet be-test` (CI enforces `gofmt`, `go vet`,
  `go build`, `go test`).

## Frontend (`frontend/`)

- Next.js 14 app router, TypeScript, no Tailwind.
- Server components call the backend via `app/lib/api.ts`
  (`QUILL_API_BASE_URL`); browser calls use the `/api/backend/*` rewrite.
- The authenticated shell lives in the `app/(app)` route group; auth-less pages
  (e.g. `/login`) sit outside it.
- Before pushing: `make fe-lint fe-build`.

## Workflow

- Foundation-first roadmap in `README.md`. One focused PR per item, each with a
  task-list checklist; keep `main` green.
- Local stack: `make up` (Postgres + Forgejo in Docker; api, dispatch & web
  hot-reload on the host) or `make stack` (full containerised stack). See
  `deploy/compose/README.md`.

## Working rules for agents

These rules apply to every coding agent in this repository. The foundry loop enforces the
ones marked **(enforced)**: breaking them throws the work away.

### Read order, every session
1. `FEEDBACK.md`: the owner's latest feedback. It overrides everything except the mission.
2. `SUITE.md` (if present: the product family this repo belongs to), `MISSION.md`,
   then this file, then `DECISIONS.md`.
3. Your task (in `.foundry/task.md` when run by foundry, otherwise the top of `BACKLOG.md`).

### Owner documents
- `SUITE.md`, `MISSION.md`, `FEATURES.md`, `USER_STORIES.md` belong to the owner. Never edit them. **(enforced)**
- `BACKLOG.md` and `JOURNAL.md` are maintained by the foundry loop; don't edit them during a task.
- `FEEDBACK.md` is written by the owner; read it, don't edit it.

### How to work
- One task per session. Keep the diff small and on-topic.
- Tests first: encode the task's acceptance check as tests, see them fail, then implement.
- `./check` is the only definition of done. It must exit 0 before you finish.
- Never delete, skip or weaken existing tests or assertions unless the task says
  "Tests may change: yes". **(enforced)**
- Never change `./check`, CI workflows or linter/type-checker configuration. **(enforced)**
- Record non-obvious design decisions in `DECISIONS.md`: date, decision, why, and the alternatives you rejected.
- Prefer the standard library and existing dependencies. Add a dependency only if it saves real work,
  and note it in `DECISIONS.md`.
- Don't run git commands that change state. The loop commits, merges and pushes.
- When something is ambiguous, pick the option most consistent with MISSION.md and
  write the question in your summary instead of stalling.

### Finishing a session
Write `.foundry/summary.md` with `## Done`, `## Questions`, and `## Follow-ups` sections.
