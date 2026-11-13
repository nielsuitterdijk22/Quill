# Serverless Quill: roadmap to a scale-to-zero platform

Status: plan, not implementation (2026-11-11). Owner-directed amendment (2026-11-11) included:
Postgres is the ref-CAS referee, bucket layout pre-supports streaming, tenant-scoped metering
and runtime portability designed in now, async maintenance as Serverless Jobs, and the
irreversible decisions called out explicitly (see "Get right the first time").

Target architecture: all compute scales to zero; git over HTTPS only (no SSH). The only
always-on floor left is the RDB Postgres instance for **Zitadel's** database (~EUR 5/mo);
the quill metadata DB stays on the scale-to-zero SDB (storage-only when idle, decided
2026-11-11) — plus object storage / Secret Manager pennies. The DEV1-S VM (Forgejo, Caddy,
act runner, Zitadel-on-VM) is fully deprecated.

Grounding (verified in-repo): Forgejo client inventory (`backend/internal/forgejo`,
~48 methods at ~80 call sites in `internal/{platform,server,auth}`), the dispatch side already
runs act as a Go library (`internal/pipeline/act_runner.go`), the VM wedge RCA
(2026-11-11, hard reboot after CI memory spike), and the SDB keep-alive lesson (Forgejo's
persistent pool billed ~EUR 22/mo CPU — predicts Zitadel-on-serverless-DB behavior).

## Forgejo call-site classification

| Class                       | Methods                                                                                                                                                        | End state                                                                                                                                                 |
| --------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Identity/org mirroring      | CreateUser/GetUser/RenameUser/DeleteUser/CreateOrg/GetOrg/DeleteOrg/GetOrCreateOrg/AddOrgMember/ListSSHKeys/Add/DeleteSSHKey/CreateAccessToken/SetUserPassword | Dropped or Quill-native in Postgres. SSH keys dropped (no git-SSH). Users already exist in `users`; org mirroring was pure Forgejo overhead               |
| Repo CRUD                   | Create/CreateUser/OrgRepo, GetRepo, EditRepo, DeleteRepo, ListOrgRepos, ForkRepo, MigrateRepo                                                                  | Quill Postgres row (already exists) + own git layer. GitHub import becomes `git clone --mirror` + bucket write                                            |
| Git reads                   | ListBranches/ListCommits/GetCommit/GetCommitDiff/GetContents/RenderMarkup/GetPullDiff/UserHeatmap/Version                                                      | gitea git module (MIT) over hydrated packs. RenderMarkup switches to goldmark (new dep, DECISIONS entry). Version dropped                                 |
| PRs/issues/reviews/comments | ListPulls, GetPull, CreatePull, MergePull, reviews, issue comments, CountOpenPulls                                                                             | PR/issue model moves into Quill's Postgres (suite identity contract is already `(repo_slug, quill_pr_number)`). Diff/merge mechanics via gitea git module |
| Policy enforcement          | Get/Upsert/DeleteBranchProtection                                                                                                                              | Quill already owns policy tables; enforcement moves into the receive-pack pre-receive hook                                                                |
| CI plumbing                 | system webhooks -> dispatch on VM                                                                                                                              | Replaced by post-receive hook -> broker (Phase A)                                                                                                         |

## Get right the first time (irreversible decisions)

These four must be correct before Phase B merges anything, because later phases build on
them and migrating after real data lands means re-writing every manifest ever written:

1. **Manifest format** — the single pointer to truth for a repository. Versioned from day
   one; parsers must tolerate unknown fields forward-compatibly (lenient decode).
2. **Ref-CAS semantics** — Postgres is the correctness referee for ref updates from day
   one (one row per repo, `SELECT ... FOR UPDATE` compare-and-swap). S3 `If-Match` on the
   manifest object is an optional _fast path only_, never the source of truth: even when
   enabled, the Postgres row is the commit point and the manifest object is a reproducible
   view of it. This keeps correctness independent of a platform feature Scaleway may change.
3. **tenant_id propagation** — every repo row, push record, CI job row, and the manifest
   itself carry `tenant_id`; metering (storage bytes, pushes, CI minutes) is tenant-scoped
   in the schema from the start, not retrofitted. Aligns with the existing tenant-scoped
   gates (`SumPipelineMinutesByTenant`, audit-log tenant scoping).
4. **Bucket layout** — must support later streaming/partial hydration (serving packs via
   HTTP range requests without full local hydration). Do not build streaming now; do not
   preclude it: packfiles stay whole, individually addressable objects (never
   concatenations or archive formats); the manifest records per-pack size/etag; the read
   path consumes packs through a `PackSource` interface so a range-serving implementation
   can swap in later without touching callers.

Everything else in this roadmap (container sizing, broker design, runner command surface,
route names) is reversible.

## Operations during the epic (production continuity)

Production keeps running and deploying the whole time — the plan has no big-bang moment.
Everything lands additive behind config flags or as new resources; the only visible
cutovers are DNS changes (git, auth) and per-repo migration windows. Rules for the whole
epic:

- **Normal cadence**: every PR merges to main, CI deploys (sha-pinned), live e2e +
  `purge-e2e` after each deploy. Nothing freezes; unrelated feature work can interleave.
- **Warm fallback per phase**:
  - Phase A: the VM dispatch stays as shadow/fallback for one week; a bad CI run means
    flipping `QUILL_PIPELINE_DISPATCH_URL` back to the VM.
  - Phase B: all git-engine code deploys inside the api container but stays inert —
    flags off by default, `git.<domain>` still points at Forgejo. Nobody sees the new
    engine until the Phase D DNS cutover.
  - Phase C: auth cutover is one DNS change; the VM Zitadel stays stopped-but-kept for
    two weeks — restart + repoint is the full rollback.
  - Phase D: repos migrate one at a time behind a read-only window (minutes, off-hours);
    Forgejo stays up and serving until every repo reports migrated+verified.
- **Rollback is always revert-the-commit + flag/DNS flip**, never a data restore — which
  is why no PR deletes what it replaces; teardown (D2) waits for a green dogfooding week,
  and the VM volume is kept two weeks beyond that.
- **Accepted degradation** (no external users yet): seconds-long cold starts at cutover,
  read-only minutes per migrating repo, occasional windows. Zero-downtime choreography
  would cost more than it protects at dogfooding scale — revisit when real users appear.
- **Standing hazards** (house rules): no cron watch-loops while iterating (they keep the
  api hot and burn SDB CPU); VM maintenance only via `docker compose up -d --no-deps`
  so forgejo/caddy/zitadel are never dragged along (the 2026-11-11 RCA).

## PR 0 — GO/NO-GO SPIKE (blocking; findings + DECISIONS entry only)

_Status: EXECUTED 2026-11-11 — result GO. Probe committed at `scripts/spike-pr0/`; findings in
DECISIONS.md. Key results: conditional writes confirmed (fast path enabled, Postgres referee
unchanged); Range GET 206 partial confirmed (streaming premise holds); `/tmp` in Serverless
Jobs is RAM-tmpfs (a 700 MiB write OOM-killed a 512 MiB-memory job) while `/mnt` is the
disk-backed local-storage volume; job egress to S3 + api works under gVisor; go/static
binaries + git run fine. Deferred to their phases: full ci.yml-through-a-job (A3), container
cold-start vs SDB-idle pairing, Zitadel pool behavior (C1)._

One Go probe program, run with live creds. The ref-CAS decision is already made (Postgres
referee); the spike verifies the rest:

1. **S3 If-Match behavior** on Scaleway Object Storage — as the _optional fast path_
   only: PutObject with `If-Match` / `If-None-Match: *`, observe 412 vs overwrite, and
   whether ETags are stable across reads. If unsupported, we simply run without the fast
   path; no fallback work is needed because Postgres is already the referee.
2. **Networking**: Serverless Container reachability of RDB (private network `pn_id` vs
   public+TLS endpoint) and Object Storage; Job -> api callback path (the VM-internal
   `dispatch:8090` goes away — the job needs an egress URL).
3. **Runtime reality check**: `/tmp` writability + size (`local_storage`) in job and
   container images for pack hydration and act's action cache; git binary present in the
   final images; gVisor sandbox limitations for the CI job (no privileged ops).
4. **Cost baselines**: one cold start and one CI round (the owner's real 2-job ci.yml) in
   GB-seconds; RDB instance price for the Zitadel DB (the ~EUR 5/mo floor); confirm the
   quill SDB CPU stays 0 while the api idles at min_scale=0 (decision already made: the
   quill DB stays on the SDB).
5. **Zitadel**: scale-to-zero behavior — DB pool footprint (the Forgejo lesson suggests an
   RDB instance for the Zitadel DB), cold-start time, SMTP via the existing admin-API
   script (`scripts/zitadel-smtp.sh`).

Risks: none (read-mostly; one test write to a scratch bucket). Tests: the probe is the test.
Rollback: n/a. Exit criteria recorded in DECISIONS.md: networking mode, RDB sizing for
the Zitadel DB (the quill SDB stays), job feasibility, If-Match fast-path enablement.

## Phase A — CI off the VM (first feature PRs; independent of the git engine)

Rationale: the VM wedge is the live operational fire (2026-11-11 hard reboot), and every
item here works while Forgejo still exists.

CI worker shape — **resolved by implementation (2026-11-11, DECISIONS.md):** act v0.2.89
executes every step through the Docker daemon (no host/"in-process" mode exists), and gVisor
cannot host Docker — so the worker is **not** a Serverless Job but an **ephemeral Scaleway
instance** ensured by the broker: cloud-init installs Docker and runs the ciworker container
(the queue-draining Go binary with act as a library — the same `internal/pipeline` runner
code that runs the owner's ci.yml on the VM today); the worker self-terminates the instance
on idle (Terminator hook; the broker TTL-kills anything wedged >3h), keeping the original
gitea-act-runner `--ephemeral` idea rejected: it registers over the Forgejo actions protocol
this roadmap deletes. Same-binary portability holds: the ciworker image runs unmodified in
warm pools later with self-termination disabled. The 2026-11-11 spike's /mnt findings stay
relevant only for the future job-based maintenance work, not the CI worker.

**PR A1 — Job queue in Postgres.** _Status: SHIPPED 2026-11-11 (flag off, inert). Migration
000024 pipeline_queue; claim/heartbeat/report at /api/v1/internal/dispatch/* (HMAC, same
dispatch secret); QUILL_PIPELINE_QUEUE_MODE gates the trigger path; SKIP LOCKED claims,
10-min visibility timeout + heartbeat, tenant stamped at enqueue. Tests-first, all green;
rollback = flag stays off. Worker = PR A2/A3._ Scope: queue rows for pipeline jobs
(carrying tenant_id for metering from day one); dispatch handler refactored from "execute
immediately" to "enqueue + return run id" using `SELECT ... FOR UPDATE SKIP LOCKED`;
API gains `POST /api/v1/internal/dispatch/claim` (HMAC, same `dispatch_secret`).
Risks: queue semantics (visibility timeout) in Postgres without a queue library — keep it
dumb (claimed_at + heartbeat). Tests: integration gated on `QUILL_TEST_DATABASE_URL`; live
pipeline round-trip. Rollback: revert; VM dispatch path kept behind a config flag until A3.

**PR A2 — Broker container.** _Status: SHIPPED 2026-11-11 (code + local round trip);
deploy wiring SHIPPED 2026-11-13 (`ci_broker.tf`, gated by `var.ci_queue_mode`; no cron sweeper —
see DECISIONS)._ Scope: new Serverless Container `dispatch-broker`
(min_scale 0), HMAC-verified `/wake` surface (same `X-Quill-Dispatch-Signature`
scheme/secret as the dispatch API), single-flight ensure of ONE tagged ephemeral worker
instance via `scaleway-sdk-go` v1.38.0 (list-before-create; TTL-kill for instances stuck
>3h; cloud-init carries Docker install + the ciworker container run). Local verification:
the full queue round trip (enqueue → signed wake → claim → apply-style heartbeat → act
execution of a real workflow in Docker → report → run/steps recorded green) passed against
a local api + scratch Postgres. Remaining deploy work: terraform for the broker container +
cron sweeper hitting `/wake` every few minutes (missed-wake backstop), registry image.
Risks: broker scale-out races (covered by the SKIP LOCKED claim); per-job startup latency
(~1–2 min instance boot on cold wake, acceptable). Rollback: flip `QUILL_PIPELINE_QUEUE_MODE`
off — the api returns to inline VM dispatch.

**PR A3 — CI worker.** _Status: SHIPPED 2026-11-11 (code + local round trip); deploy wiring
SHIPPED 2026-11-13 (worker = the backend image's `/quill-ciworker`, pulled by cloud-init)._ Scope — amended by the act-no-host-mode finding (DECISIONS.md): **an ephemeral
Scaleway instance, not a Serverless Job.** The ciworker is a plain Go binary
(`cmd/ciworker`) draining the queue (claim via API → heartbeat → run act on the instance's
host Docker → report; CI minutes metered against the job's `tenant_id`), taking its workdir
and act cache dir from env (portability rule kept: runs unmodified in warm pools later;
self-termination — poweroff → poll stopped → delete — is a Terminator hook driven by
SCW_INSTANCE_ID, disabled when absent). Workspace clone still comes from the Forgejo URL
until Phase B. Deploy work pending: `Dockerfile.ciworker` (backend build + toolchains:
go, node 22, git; `--platform=linux/amd64`), terraform for the IAM instance key, and the
ciworker image push from the deploy workflow. Risks: log streaming latency; workflow steps
needing an image build — kaniko baked into the runner image when first needed (decided
2026-11-11: kaniko first, a dedicated buildkit backend as the later upgrade), deferred
until a repo actually builds images — follow-up. Tests: unit (drain loop, fake queue with
HMAC validation; terminator poll loop) + the local Docker round trip done; remaining:
live-run the owner's real ci.yml through an ephemeral instance and compare with a VM run.
Rollback: keep the VM dispatch active as shadow/fallback for one week (flag stays off);
then remove the act runner + dispatch from the VM compose/cloud-init (forgejo, caddy,
zitadel stay until their phases).

## Phase B — the own git engine

**PR B1 — Storage model + provisioning.** _Status: SHIPPED 2026-11-12 (flag off, inert;
deploy wiring when the cutover era needs it)._ Bucket layout under a future `repos`
bucket, designed for later streaming (see "Get right the first time"):

```text
repos/{tenant_id}/{project_slug}/{repo_slug}/
  manifest.json          # small, versioned, tenant_id + refs + packs index
  packs/<pack-sha>.pack # standard git packfiles, one object per pack, whole and
  packs/<pack-sha>.idx  #   addressable — range-requestable by construction
  tmp/…                 # in-flight receive-pack objects, garbage-collected by maintenance
```

Manifest (JSON, schema-versioned, lenient decode): `version`, `tenant_id`, `default_branch`,
`refs[{name,target,peeled}]`, `packs[{object,bytes,pack_size,etag}]`,
`push_history` tail (`pushed_by`, `tenant_id`, `pushed_at`, `sha`), `updated_at`,
`storage_bytes`. Referee: Postgres row-per-repo CAS (`SELECT ... FOR UPDATE`) as the
commit point; S3 `If-Match` fast path only if the spike confirms it. `CreateRepo` stops
calling `CreateOrgRepo`; init = empty manifest write + Postgres row (tenant_id stamped).
Schema also gains the runtime routing hint: `tenants.runtime_hint ('serverless'|'warm')`
— unused now, consulted later when warm pools exist. Risks: manifest format churn —
version from day one; this is an irreversible decision. Tests: unit for referee CAS
(loop with simulated lost races), integration for create-repo. Rollback: Forgejo path
still behind config toggle.

_Shipped shape (2026-11-12, DECISIONS.md): manifest fields slightly refined from the
sketch — packs carry `{object, bytes, etag, idx_object, idx_bytes, idx_etag}` (the
sidecar idx gets the same first-class metadata the pack gets; "pack_size" folds into
"bytes"); a golden test pins the exact wire format. Referee realized as a monotonic
`revision` counter on the `git_repo_state` row: writers `UPDATE ... WHERE
revision = <expected>` (single-statement CAS, 0 rows = ErrRefCASLost), with the refs
map as jsonb. Provisioning is own-mode only behind `QUILL_GIT_ENGINE_MODE=own`
(+ `QUILL_GIT_S3_*` env; config validates completeness at startup): manifest written
create-if-absent (`If-None-Match: *`), repo row + referee row committed in one
transaction (raw `InTxTx`), DB failure compensated by deleting the still-empty
manifest. New `internal/objectstore` (SigV4 lifted from the proven PR-0 spike,
stdlib-only — no new deps) + `internal/gitengine` (manifest/layout/engine);
`objectstore.MemStore` doubles the bucket in unit tests, so B2/B3 tests never need
credentials either. CloneFrom imports under own mode fail explicitly (D-era surface).
Live-verified: the objectstore conditional-write contract ran green against the real
Scaleway bucket (gated `QUILL_TEST_S3_*` roundtrip test)._

**PR B2 — Read path (smart HTTP v0 + v2).** _Status: SHIPPED 2026-11-12 (flag off;
clone proven locally over v0 and v2; git.<domain> DNS move lands at Phase D)._ Scope:
the smart-HTTP read surface `git.<domain>` becomes a second custom domain on the api
container (matches "existing permission middleware"); routes
`/{owner}/{repo}.git/info/refs|git-upload-pack` — shipped at top-level prefix
`/git/{owner}/{repo}/...` (suffix URL param, optional `.git`), repointed/mapped at the
DNS cutover. First-access hydration: GET manifest -> download packs to the configured
work dir -> serve via upstream `git upload-pack` (shelled out; upstream git does the
work). Spike-adjusted (2026-11-11): in the api container
the writable layer is RAM against `memory_limit` (containers get no disk volume like the
jobs' `/mnt`), so hydration size must fit the container memory budget — fine at dogfooding
sizes (free-tier cap 1 GiB; pin `container_memory_mb` accordingly) and the forcing function
that eventually prioritizes the `PackSource` streaming implementation. Warm pools (Kapsule)
later get a real disk work dir instead. Packs are consumed through the `PackSource`
interface — full-download implementation now, range-request/streaming implementation later
without caller changes. Risks: cold-start latency on first clone (seconds, acceptable);
memory ceiling for big repos (above). Tests: gated on `QUILL_TEST_DATABASE_URL` and
`QUILL_TEST_S3_ENDPOINT`: real `git clone` against an httptest server backed by a seeded
fixture repo; hydration idempotence; manifest-format golden test. Rollback: new host only —
`git.<domain>` DNS repoints; Forgejo untouched during B.

_Shipped shape (2026-11-12): `gitengine.PackSource` (full-download impl; repo-relative
manifest pack objects resolve against the repo's bucket prefix), `RepoCache.Hydrate`
(skeleton `git init --bare`, refs re-materialized from the manifest into packed-refs
EVERY hydration — a stale cache can never serve moved refs — packs size-matched and
reused), and `SmartHTTP` (pkt-line framing + `git upload-pack --stateless-rpc`, v2 via
`GIT_PROTOCOL` passthrough; push is refused until B3). Tests run REAL `git clone` over
the handler (v0 forced and v2 default): fixture repo built + repacked + uploaded to
MemStore, cloned back, files/tag/HEAD-sha asserted against the manifest; hydration
idempotence proven via a counting store (no pack refetch, byte-identical packed-refs).
Server wiring: `/git` mounted only when the engine flag is on; resolution via an
interim unscoped slug resolver (`FindRepoForGit`) — repos still on Forgejo have no
manifest and 404 naturally; B6 swaps in token-scoped auth (no cutover before that)._

**PR B3 — Write path.** Scope: `git-receive-pack` route; policy checks in pre-receive
(branch protection from Quill's policy tables, free-tier gates); single-writer
serialization via Postgres advisory lock per repo (acquired before receive-pack, readers
unaffected); pack write to `packs/` under a content-addressed tmp key; ref update commits
in Postgres first (referee), then manifest object write (If-Match fast path when enabled);
push identity recorded in the manifest `push_history` and the audit log, both tenant-scoped;
size metering switches from Forgejo repo size to manifest `storage_bytes` (per-tenant
storage metering becomes exact). Risks: lock lifetime vs long pushes (heartbeat); bounded
CAS retries — on persistent race, fail with the clean "fetch and retry" message git clients
handle natively. Tests: push / fast-forward / non-fast-forward rejection / concurrent-push
conflict (one loses cleanly) / clone-equals-original / `git verify-pack` on plain
downloads. Rollback: writes gated by config; Forgejo path remains until Phase D.

_Shipped shape (2026-11-12, DECISIONS.md): NO git hooks — the smart-HTTP handler IS
the integration point (stateless-rpc lets us parse the v0 command list before the pack
bytes, enforce, run receive-pack buffered, and commit truth before the client sees
success). Single-writer serialization via a `push_leases` Postgres ROW, not an advisory
lock: advisory locks are session-bound and a push spans processes (handler →
receive-pack → finalize), so a lease row keyed `repo_id` with holder + TTL
self-heals crashed writers without a sweeper. Flow: parse (400) → resolve+hydrate →
lease (423 when held) → referee freshness vs manifest (409 fetch-first) → pre-receive
guard → receive-pack (with `receive.unpackLimit=1`: incoming packs must STAY packs —
loose objects would never reach the bucket; `gc.auto=0`: no background gc on a
disposable cache) → finalize (new packs → bucket create-if-absent, manifest rebuilt
with push_history actor+tenant and storage_bytes = Σ(pack+idx), referee CAS commit
FIRST, manifest If-Match write after with bounded retries + a lease-justified
unconditional last attempt). Policy scope: pre-receive enforces what a push can
violate — `BlockForcePush` (force via `git merge-base --is-ancestor`, and deletes) —
while RequiredApprovals/RequirePullRequest/RequireStatusChecks target the merge gate
(B5). Guard rejections are synthesized v0 report-status (sideband-aware when the
client negotiated side-band): git renders its native "! [remote rejected] main (reason)"
UX, the same shape a hook decline produces. Push identity is anonymous until B6
(`ReceiveConfig.Actor` seam); push size capped at 256 MiB (413). Tests: real `git
push` over the handler — round trip (clone-equals-original across two pushes, manifest
asserts refs/packs/history/metering, referee revisions), force-guard and delete-guard
rejections leaving bucket + referee untouched, lease-held 423 (then success after
release), referee-divergence 409, advertisement served (and refused on read-only
instances), pkt-line parser edges; gated store lease tests (acquire/release/expiry
steal); gated platform guard tests (BlockForcePush: FF passes, create passes, force
and delete on `main` rejected, unprotected refs pass, mixed push poisoned; no-policy
repo unrestricted)._

_CI trigger consequence of B3 (pin for Phase D planning; noted 2026-11-12): today's
pipeline triggers ride Forgejo push webhooks into the broker's HMAC-verified `/wake`.
Once the own engine serves git, that chain has no emitter left — the push finalizes
IN our handler, with truth committing at the referee CAS in Postgres. Long-term shape:
a transactional push-event row (repo, refs old/new, actor, tenant_id) written in the
SAME transaction as the referee CAS, drained by the dispatcher — at-least-once,
atomic with truth, no HMAC hop, no webhook-loss window (broker keeps its HMAC surface
for backward compat during D). Two rules transfer to the trigger path: triggers key
refs off the referee row, never the manifest (finalize writes Postgres first, manifest
second — same rationale as B7 reconciliation), and trigger emission stays out of the
push request path (the handler writes the event; the dispatcher drains; a wedged
pipeline can never wedge a push)._

**PR B4 — Read APIs via gitea git module.** Scope: extract
`gitea.com/gitea/modules/git` (MIT) as the read engine behind
`internal/platform/browse.go`'s existing shapes (branches/commits/diff/contents/heatmap).
RenderMarkup -> goldmark. Risks: the module assumes an on-disk bare repo (it is one —
hydrated in the work dir); dependency weight — vendor only the git module, DECISIONS
entry. Tests: port browse integration tests to bucket-backed fixtures; optional parity
harness comparing Forgejo vs own output during dual-run (PR D1). Rollback: per-route
feature flags; the old Forgejo client stays until Phase D.

_Shipped shape (2026-11-12, DECISIONS.md): browse ops serve engine-backed repos by
shelling upstream git in the hydrated dir — NOT via the gitea module (the module itself
execs the same git binary; zero new deps for reads, B5 gets the decision with merge
evidence). goldmark (gitea's own markdown core) renders engine-repo markdown, no
WithUnsafe → raw HTML escaped, sanitized by construction; Forgejo repos keep the
client renderer until D. Routing: the referee row IS the discriminator — repos with a
`git_repo_state` row serve from the engine, everything else stays on Forgejo (a Phase-D
migrated repo flips browse behavior atomically with its git truth). Public-read bypass
and anonymous-404 semantics preserved verbatim; metadata calls never touch a git
backend. Tests push a REAL repo through the mounted smart-HTTP stack and then exercise
every op (branches/commits/path-filter/diff/contents+base64/tag refs/last-commits/
NotFound/empty-repo sentinel), plus the inverted-discriminator proof. Call-site
classification recorded: browse ops = own layer; ListWorkflows/GetWorkflowContent
(CI ci.yml reads) route via engine contents as a B6-adjacent follow-up; pulls → B5;
user heatmap → D (user-level, not repo browse)._

**PR B5 — PR/issue model in Postgres.** Scope: tables for PRs (numbering per repo under
the `(repo_slug, quill_pr_number)` contract), issues, review threads, comments — all rows
stamped `tenant_id`. `internal/platform/pulls.go` reimplemented on the gitea git module
(diff = git diff; merge = module merge ops + new pack/manifest write through B3
primitives). Split internally: (a) storage + list/read, (b) create/merge, (c)
reviews/comments, (d) heatmap + audit. Risks: merge correctness — lean on git's own merge
via the module helpers; UI parity — the frontend consumes these shapes unchanged, so the
diff is backend-local. Tests: PR create/merge/conflict integration suite gated on the test
DB + fixture repo; merge-gate policy tests (2-reviews rule) re-run against the new engine.
Rollback: Forgejo-backed PR handlers behind the flag until Phase D.

**PR B6 — Git auth/tenancy.** _Status: SHIPPED 2026-11-12 (flag off; the /git mount is
engines-only, so production keeps the unauthenticated interim resolver until the flag
flips)._ Scope per the design below. Shipped shape: `platform.AuthenticateGitRequest`
validates Basic credentials — primary: Quill-native `qgt_` tokens, 32 random bytes,
stored as SHA-256 + indexed lookup (no secretbox, no per-push decrypt; revocation =
row delete); secondary: Zitadel PATs via `ZitadelVerifier.Introspect` (`/oauth/v2/intro
spect` with the management token) mapped like a verified login — an unknown subject is
NOT provisioned from a git auth, the user must have signed in once; absent credentials
are anonymous (public-repo reads only). Authorization: `ResolveRepoForGitAccess` —
anonymous reads public repos (private/unknown → Basic challenge, no existence leak),
members read anything they browse, pushes need an authenticated write-level project
role ("read" members get 403). HTTP semantics: gitengine gains sentinels
(`ErrGitUnauthorized/ErrGitForbidden/ErrGitNotFound`) mapped to 401+`WWW-Authenticate`
/403/404 — the crucial rule being that an ANONYMOUS denial becomes a 401 challenge so
terminal git prompts and retries (a 404 there would strand a credential-less clone);
authenticated denials keep 403/404. Push identity: the authenticated username lands
in the manifest `push_history` (tenant + sha stamped) and in a tenant-scoped
`git.push` audit row plus a daily `git_push_meters` counter per tenant (both
best-effort, off the push's critical path). Tests: HTTP access-matrix over the real
mount (public/anonymous-challenge/member/non-member/read-role-push/owner-push/invalid
token), end-to-end credentialed push asserting manifest identity + meter + audit
ticks + anonymous clone of the public result + revoked-token push failure; the
production wiring is mirrored in the fixture (auth middleware → scoped resolve →
gitengine sentinels). Native tokens mint/revoke ride the existing
`/me/git-token(s)` routes — CreateGitToken routes to MintGitToken in engine mode;
RevokeGitToken deletes the row for native tokens and keeps the Forgejo remote-revoke
flow for legacy ones. Store: migration 000028 (`git_tokens.token_hash` + partial
index, `git_push_meters`); generated sqlc files hand-patched (wholesale `sqlc
generate` is incompatible with the repo's pinned generated output — recorded in
DECISIONS)._

**PR B7 — Async maintenance Jobs.** _Status: SHIPPED 2026-11-12 (code + admin runner;
`scaleway_job_definition` wiring SHIPPED 2026-11-13 (`git-maintenance`, daily cron) — the runner is
`quill admin git-maintenance`, pure Go, cron- or broker-triggerable, no infra needed to
start using it locally)._ As designed, with the shipped shape: `gitengine.Maintenance`
per repo — (1) reconcile: rebuild the manifest from the referee when refs diverge,
adopting bucket-listed packs the manifest lost (the B3 lost-etag-race repair; adoption
ONLY on refs-divergence — a matching view never hoards debris, which would pin garbage
into the manifest forever); (2) repack past `--repack-threshold`: consolidate into one
pack via `git repack -a -d` in a scratch hydration, re-stamp referee storage (revision
advances on the lease — refs unchanged, CAS holds, freshness is refs-equality); (3)
verify: cold-scratch hydration + `git fsck --connectivity-only` — GC is INTERLOCKED on
it (a failing verify skips deletion entirely); (4) GC: delete listed pack pairs the
manifest does not reference. Lease: the same `push_leases` row a push takes (in-flight
push → clean skip). Listing: `ListObjectsV2` added to the S3 client (stdlib XML; SigV4
signer extended with canonical query strings) + MemStore parity; the engine request
paths never list. Tests: idempotence (run twice = zero work, byte-identical bucket),
lost-manifest-write repair (re-link + clone/rev-parse/fsck on the repaired view), orphan
GC (referenced pack survives, debris deleted), repack (incremental-packs fixture → one
pack, referee storage equal to manifest, cold-cache fsck, second run no-op), lease
respect, and verify-interlock (refs without objects → error, nothing deleted).
Rollback: harmless if it never fires — orphans cost pennies (as designed).

## Phase C — Frontend decision + Zitadel serverless

**Frontend — decided: keep the Next.js Serverless Container, min_scale 0.** Static export
is not feasible: Auth.js session handling, server components calling the api, and the
`/api/backend/*` rewrite all need a Node runtime per request. Bucket+CDN would require
rewriting auth into the api and losing server components — a rewrite with no cost win
(a min_scale=0 container is free when idle). Record in DECISIONS.md. No PR.

**PR C1 — Zitadel container + setup job.** Scope: move the existing `quill-zitadel` image
from the VM compose into a Serverless Container (min_scale 0, custom domain
`auth.<domain>` — the record already exists, so DNS is an A-record change to the container
CNAME); a separate `scaleway_job_definition` `zitadel-setup` runs `zitadel setup` + the
SMTP admin-API script, invoked by CI post-apply — never in the container boot path (cold
start must not race migrations); masterkey in Secret Manager (already generated as
`random_password.zitadel_masterkey`); DSN per the spike outcome (expected: RDB instance,
private network). Risks: cold-start login latency (seconds, acceptable); the persistent
pool lesson is exactly why Zitadel's DB goes to RDB, not SDB; JWKS-rotation handling
already shipped (2026-11-10) and the api's `/readyz` JWKS gate covers cold starts.
Tests: live sign-in round + the full live e2e suite; monitor DB CPU for a week before
removing the VM copy. Rollback: VM Zitadel stopped but kept for two weeks — repoint DNS
and restart.

## Phase D — migration + VM teardown

**PR D1 — Migration tool** (design below), run repo-by-repo with dry-run first.

**PR D2 — Cutover + teardown.** Scope: switch pipeline triggers to Quill's post-receive
hook (drop the Forgejo system webhook), run the live e2e suite + one dogfooding week, then
terraform: delete `forgejo_vm.tf` resources, the VM's dispatch, the `ssh.` DNS record, the
VM compose stack's zitadel/forgejo, the Flexible IP. The floor afterwards: **the RDB
instance for Zitadel's DB (~EUR 5/mo) — the quill metadata DB stays on the scale-to-zero
SDB (storage-only when idle) — + backup object storage (cents) + Secret Manager versions
(cents)**; per-request costs = container invocations (fractions of a cent; cold starts add
seconds of GB-time) and CI jobs (a 2-job, ~4-min workflow at 2 GB ≈ single-digit cents;
measure in the spike). Rollback: none past terraform destroy except backups — hence the VM
root volume (`delete_on_termination=false`) is kept for two weeks; a restored Forgejo VM is
the full-fidelity fallback until the volume is finally deleted.

## Auth/tenancy design (PR B6)

- **Web users**: unchanged — Auth.js confidential OIDC client against `auth.<domain>`,
  JWT sessions on the api.
- **Git endpoint** (`git.<domain>` -> api container), HTTPS Basic auth with two credentials:
  1. **Quill-issued git tokens** (primary): scoped personal tokens created in settings
     (stored hashed via the existing `secretbox`), validated locally against project
     membership. Rationale: no per-push round-trip to a scale-to-zero IdP; revocable,
     auditable per token; git availability decoupled from Zitadel cold starts.
  2. **Zitadel PATs** (secondary): Basic header introspected at Zitadel behind the
     `AuthProvider` interface (no direct provider calls — add an `Introspect` method).
- **Authorization**: the existing project/repo permission middleware resolves
  `{tenant, project, repo}` before any git route; read = repo read, push = write role;
  tenant isolation rules unchanged (MISSION hard constraint).
- **Push identity**: authenticated Quill user recorded in manifest `push_history` and the
  tenant-scoped audit log; commit authorship is not overridden (standard git semantics).
- **Provisioning**: `CreateRepo` = Postgres row + empty manifest — no org/user mirroring
  anywhere; the Forgejo identity-mapping columns (`forgejo_org_name`, `forgejo_username`)
  die in D1's migration, not before (live probes still use them).
- **Tenant metering**: pushes, storage bytes (manifest sum), and CI minutes metered per
  `tenant_id` from day one, feeding the existing freemium gates (`SumPipelineMinutesByTenant`,
  storage gate).

## Repo migration tool (PR D1)

`quill admin migrate-forgejo-to-bucket`:

- Enumerate repos via the Forgejo admin API; per repo: `git clone --mirror` from the VM
  (file path over SSH), repack, upload packs + manifest with refs preserved, default-branch
  heuristics.
- `--dry-run`: per-repo size/ref/PR-count table, writes nothing.
- Verify pass: after upload, `git rev-parse` every ref against both sources + `git fsck`
  on the uploaded packs, object-count equality.
- Post-verify: PR/issue/review/comment history migration into the new Postgres model
  (numbers keep the `(repo_slug, quill_pr_number)` contract).
- Idempotent/resumable per repo; a repo migrates read-only (flag in Postgres) during its
  window; Forgejo stays up until every repo reports migrated+verified.
- Rollback: the bucket copy is additive; Postgres retains the `forgejo_*` columns until
  D2, so a repo can be flagged back to Forgejo.

## Terraform changes summary (per phase)

- A: + `dispatch-broker` container (api-addressed), CI `job_definition`, job IAM keys;
  `QUILL_PIPELINE_DISPATCH_URL` moves from the VM IP to the container route.
- B: + `repos` bucket + IAM key; + `git.<domain>` `scaleway_container_domain` on the api.
- B7: + `git-maintenance` job definition (cron + broker-triggered).
- C: + zitadel container + `auth.<domain>` domain + `zitadel-setup` job; the deploy
  workflow gains a post-apply setup step.
- D: − `forgejo_vm.tf` (instance, security group, cloud-init), − VM backup sidecar,
  − ssh record, − dispatch infra on the VM. DNS: `git` + `auth` A records -> container
  CNAMEs at Cloudflare (manual, documented like the 2026-11-06 migration; the deploy
  workflow's domain pinning stays literal — no `vars.QUILL_DOMAIN` revival).
- Ordering rule: nothing is deleted in the same PR that adds its replacement; teardown is
  its own PR after a green dogfooding week.

## Test strategy (gated like existing suites)

- Referee/hydration/push logic: integration tests skip unless `QUILL_TEST_DATABASE_URL`
  (existing pattern) **and** `QUILL_TEST_S3_ENDPOINT` (+creds) are set; a dockerized MinIO
  in `make up` gives every dev the S3 side. New deps chosen at B1 and recorded in
  DECISIONS.md: an S3 client (`aws-sdk-go-v2` or `minio-go`) and the gitea git module (B4).
- Real-git protocol tests: drive the `git` binary against `httptest` servers — push, clone,
  CAS conflict (including Postgres-referee race), hydration-from-cold, gitea-merge PR
  flows.
- Inspectability: a test asserts downloaded `packs/*.pack` pass `git verify-pack` and the
  manifest parses against the documented format (format doc lives next to the code, not
  just in DECISIONS).
- Runtime portability: the git engine and runner images are exercised locally in plain
  Docker (the "warm pool" stand-in) and in the Serverless runtime in the live e2e round —
  same image, same behavior.
- Live e2e (`QUILL_LIVE_TEST=1`): the git round-trip already exists — add
  PR-from-own-engine, CI-broker round, cold-start clone timing; keep purging with
  `purge-e2e`.
- Cost watch: after D2, one billing check via `scw billing consumption list`; no standing
  cron watch-loops (cost-watch rule).

## Open questions / resolved questions

Resolved 2026-11-11 by the owner:

- **quill DB stays on the SDB** (EUR 0 idle; the api at min_scale=0 demonstrably keeps
  its SDB cold). Only Zitadel's DB moves to a fixed RDB instance — Zitadel holds persistent
  connection pools, the exact behavior that made Forgejo's SDB cost ~EUR 22/mo.
- **Docker builds in user workflows are deferred, not dropped.** Two different
  "docker build"s exist here: (1) building the platform's own images (backend, web,
  zitadel, ci-runner) — that stays in the GitHub deploy workflow with ordinary
  `docker build`, unchanged by this roadmap; Serverless Containers/Jobs receive ready-made
  images pinned by sha, as today. (2) a _user's pipeline step_ running `docker build` —
  that worked on the VM only by borrowing the VM's Docker daemon, and the gVisor job
  sandbox has none. No current workflow does (2), so nothing breaks; when a repo needs
  image builds, the daemonless builder of choice is **kaniko** (decided 2026-11-11), with a
  dedicated buildkit deployment as the possible later upgrade if kaniko's limits bite
  (remote caching, complex builds).

To be answered by the spike (PR 0):

1. S3 If-Match verdict — **resolved 2026-11-11: supported; fast path enabled** (Postgres
   referee unchanged; Range 206 partial also confirmed).
2. Whether act-as-library (the chosen CI worker shape, see Phase A) executes workflows
   correctly under the gVisor job sandbox — partially answered by the spike (go static
   binaries, git, S3 and api egress all work under gVisor); the full proof stays in A3
   (running the owner's real ci.yml through a job). Fallback on unfixable failure:
   ephemeral runner registration, with the added cost of a compatible registration
   endpoint.
