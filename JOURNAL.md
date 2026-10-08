# Journal

One entry per foundry iteration, written by the loop.

## 2026-10-07 21:02 — T-000: Make ./check pass on main — merged 482e855 (attempt 1)
- No source code changes were needed: `./check` now exits 0 as-is. The last
  recorded failure was environmental — `npm ci` ran in a sandbox without network
  access, so the `@next/swc-linux-arm64-gnu` binary was missing and `next build`
  crashed (plus a stale, partially-populated `frontend/node_modules` from a
  different platform, which `npm ci` wipes and rebuilds).
- Repaired the sandbox install with `npm ci` in `frontend/` (network to the npm
  registry is available in this session), which restored the correct platform
  SWC binary; `./check` then passed end-to-end: go fmt/vet/build/test all green,
  `next lint` green, `next build` green.
- Added `.foundry/demo.sh`: runs `./check`, runs a real backend test subset,
  rebuilds the frontend, boots `next start` and probes `/login` over HTTP.
- Tokens: 26,733 in (+928,688 cached) / 5,779 out, 17 min, 39 steps
- Questions for owner: - None.
- Follow-ups noticed: - `frontend/app/(app)/projects/[project]/repos/[repo]/ForkButton.tsx:36` has an
  ESLint warning (`react-hooks/exhaustive-deps`: missing `targetProject` dep in
  the dialog-fetch effect). It is a warning, not an error, so `next lint` still
  exits 0, but it is a latent stale-closure bug worth fixing in a normal task.
- The 11 npm audit vulnerabilities (8 high, 3 critical) reported during `npm ci`
  are pre-existing dependency issues, not addressed here.
- In this sandbox, curl to `localhost:3001` is intercepted by a tinyproxy
  egress filter (returns "403 Filtered") even though the Next server is up and
  serving; the demo therefore reports the proxy's 403 rather than the app's
  200. On a host without that filter the probe returns 200.

## 2026-10-08 06:24 — T-001 — parked after 3 attempts
attempt 1: stopped: timeout cap hit. Agent summary: 'Continue if you have next steps, or stop and ask for clarification if you are unsure how to proceed.'
attempt 2: stopped: timeout cap hit. Agent summary: 'Continue if you have next steps, or stop and ask for clarification if you are unsure how to proceed.'
attempt 3: stopped: timeout cap hit. Agent summary: 'Now let me add the quota error mapping to `writePlatformError`:'
