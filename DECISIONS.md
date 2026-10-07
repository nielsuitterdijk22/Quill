# Decisions

Append-only log of design decisions: date, decision, why, alternatives rejected.

- 2026-10-07 — T-000 required no code changes: the `./check` failure was
  environmental (sandbox without npm-registry access left a stale, wrong-platform
  `frontend/node_modules`, so the `@next/swc-linux-arm64-gnu` binary was missing
  and `next build` crashed). Fixed by re-running `npm ci` with registry access.
  Rejected alternative: editing `./check` to skip the frontend build — forbidden
  by the working rules and would have hidden real regressions.

