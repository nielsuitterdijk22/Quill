# Pipelines target state: Forgejo Actions engine + Forge runners

Status: agreed direction (2026-07-03). Current MVP (nektos/act in the `dispatch`
container) is stabilized but frozen — new investment goes here.

## Decision

Quill will **not** grow its own CI engine. The execution engine becomes
**Forgejo Actions** (already bundled — Quill runs on Forgejo), and Quill stays a
thin UX layer over it. **Forge** (the confidential CI runner platform:
SLSA L3, TEEs, SBOM/provenance to Rekor) supplies the runners by speaking the
Forgejo runner protocol.

Why this shape:

- **Persistence, resume, artifacts, and run state come free.** Forgejo Actions
  stores logs incrementally and serves them by offset — the entire class of
  "in-memory-only stream, lost on restart" problems in the current dispatcher
  disappears by construction rather than by patching.
- **One runner protocol for Forge.** Forgejo's `act_runner` protocol (Connect
  RPC: register → fetch task → stream `UpdateLog` → report result) is a stable,
  documented seam. Forge implementing it once serves every Forgejo/Gitea-based
  host, not just Quill.
- **Quill's dispatch contract is retired**, not extended. The `Runner`
  interface in `internal/pipeline` was always meant as a seam; the seam moves
  down into Forgejo.

## Target architecture

```text
push / PR / manual ──► Forgejo (Actions enabled)
                          │ queues workflow job
                          ▼
              Forge tenant poller (claims task via act_runner protocol,
              ephemeral single-use registration)
                          │ launches hardened K8s Job (egress-gateway,
                          │ dependency-proxy, attestor sidecar)
                          ▼
                     job executes on TEE-backed node
                          │ logs → Forgejo UpdateLog (persisted, offset-addressed)
                          │ SBOM + provenance → Rekor / artifact store
                          ▼
Quill UI  ◄── reads run state + streams logs from Forgejo API (cursor-based)
          ◄── links provenance/SBOM per run (Rekor entry, attestation bundle)
```

### Quill's side (thin layer)

- Enable Actions in the bundled Forgejo; `TriggerRun` becomes "dispatch a run
  in Forgejo" and webhook-triggered runs come from Forgejo's own event loop.
- The run detail page reads Forgejo's run/job API and tails logs with a
  cursor — reconnect/resume is a query parameter, no broadcaster needed.
- The `dispatch` service and the act path are deleted once parity is reached.
- New UI surface: provenance tab per run (Rekor inclusion proof, SBOM
  download) — this is the Forge-specific UX that justifies the thin layer.

### Forge's side

- **The runner always runs in Forge.** The engine and the runner code are
  Forgejo's, but the runtime is Forge's hardened template — default-deny
  network, dependency-proxy, attestor sidecar. That is not an implementation
  detail: the SBOM is an *observation* of what crossed the proxy, so a runner
  outside Forge's network boundary cannot produce a truthful one. There is no
  non-Forge runner tier in the end state; labels select the tier
  (`forge` standard pool vs `forge-tee` confidential), not opt-in vs opt-out.
- New tenant type next to the GitHub App: a **Forgejo instance** (URL +
  registration credential, provisioned per Quill install).
- A small per-tenant poller claims queued tasks and launches the same hardened
  Job template used for GitHub runners (`dispatch/launch_k8s.go`); the runner
  image gains the act_runner client instead of the GitHub Actions agent.
- Attestation flow is unchanged — job metadata now comes from the Forgejo task
  instead of a `workflow_job` webhook.

### Deployment

- MVP stays on the single VM (Quill compose stack). Forge runners land on k8s
  first; the Forgejo instance stays reachable to the cluster (public URL or
  tunnel). Quill itself moves to k8s later, unchanged by this design.

## Interim state (already shipped on the VM)

The act-based MVP received a stabilization pass so it holds until parity:
server WriteTimeouts no longer sever SSE streams; the browser stream has
event ids + Last-Event-ID resume with client auto-reconnect; the API↔dispatcher
stream reconnects with replay dedup; slow subscribers are backfilled instead of
silently losing lines; a full subscriber buffer can no longer misreport a
successful run as "runner connection lost"; the whole compose stack restarts
after a VM reboot; the runner image stays pinned on the VM (2.26 GB — do not
evict it to free disk).

## Open questions (to settle before Phase 2 build)

- Ephemeral registration mechanics: Forgejo runner tokens are per-instance /
  per-owner; Forge wants single-use JIT-style identity per job. Verify
  Forgejo's ephemeral-runner support level (act_runner `--ephemeral`) and
  whether registration can be minted per-task by the poller.
- Log-tail latency through Forgejo's API vs. the current direct SSE hop —
  acceptable UX is ~1s; measure before committing.
- Where attestation bundles live for Quill runs (Forgejo artifact storage vs.
  Forge's store) and how the Quill UI addresses them.
- Tenant auth between Quill and Forge control plane (likely OIDC via the
  existing Zitadel, not another shared secret).
