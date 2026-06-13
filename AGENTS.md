# Repository guidance for coding agents

Quill is a VCS platform (Go backend + Next.js frontend) layered on Forgejo. This
file orients automated contributors. Keep changes small, typed, and tested.

## Golden rules

- **Forgejo is wrapped, never forked.** Git/repo/PR primitives go through Forgejo's
  REST API (`internal/forgejo`, added in PR 4). Quill's Postgres holds only the
  platform metadata Forgejo can't: orgs/teams, ownership, branch policies,
  pipelines, and auth identity mapping.
- **Auth stays behind an interface.** Never call a specific provider directly;
  go through `AuthProvider` (PR 3) so OIDC providers drop in later.
- **Namespaces are shallow but nest-ready.** Org → Repo + Teams + owning team.
  The `namespace` table carries `parent_id`; don't hardcode a flat assumption.
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
- Local stack: `make up` (Forgejo + Postgres + api + web). See
  `deploy/compose/README.md`.
