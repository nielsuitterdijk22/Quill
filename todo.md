# Quill — todo

The previous round of UX/feature fixes has landed on `main`. This list is a fresh
pass written after reviewing the codebase. Items are grouped by type; each is
meant to be a small, focused change in keeping with the foundation-first roadmap.

## Open design decisions (need a call before building)

- **Namespace vocabulary & depth.** Settle the model across Quill / Forge / Yaly.
  Today it's `Org → Repo` with Teams + an owning team; the `namespace` table
  already carries `parent_id` for future nesting. "Org > Team > Project" reads
  awkwardly and doesn't capture "one company, many orgs/groups". Decide the
  canonical hierarchy and naming, then align UI copy + routes. Don't mass-rename
  until the model is agreed.
- **Identity mapping for self/author checks.** The PR page hides the review form
  by comparing the Quill `username` to the Forgejo `pull.author.login`. That
  holds while Forgejo accounts mirror Quill usernames, but breaks once OIDC
  providers (Keycloak/Entra/GitHub) issue different logins. Decide whether the
  backend should return an explicit `viewerIsAuthor` flag (preferred) instead of
  comparing identifiers in the frontend.

## Follow-ups from the recent fixes

- **Git token reconciliation.** If recording a freshly minted token fails *and*
  the compensating Forgejo delete also fails, an orphaned Forgejo token is left
  behind (logged, not user-visible). Add a reconcile path — e.g. list tokens
  from Forgejo and surface/clean any `quill-git-*` tokens with no DB record.
- **Token list metadata.** We store only name + created_at. Consider surfacing
  scope and last-used (Forgejo exposes these) so users can audit tokens, and
  warn on duplicate display names (only `forgejo_token_name` is unique today).
- **README render caching.** Every repo/tree page render does a POST to the
  backend → Forgejo `/markup`. Cache the rendered HTML (per repo + content hash)
  or render once server-side to avoid repeated round-trips on navigation.
- **Integration coverage for git tokens.** The create/list/revoke flow has no
  test. Add a gated integration test (behind `QUILL_TEST_DATABASE_URL`) covering
  the store queries and the revoke path, mirroring the existing platform tests.

## Roadmap — PR 8: Pipelines (in progress)

- Finish runner integration: runs, **logs streaming**, and **status checks** on
  PRs (the overview now lists workflows + last run, but run detail/logs and the
  PR merge-gate wiring are still thin).
- Surface pipeline run status on the PR conversation page so required checks
  participate in the merge gate alongside branch policies (PR 7).

## Smaller polish

- Confirm the new `/markup` endpoint's 512 KiB input cap is right for large
  READMEs and document it.
- Empty-state copy for the git-token list (no tokens yet) and for the pipelines
  overview when a repo has workflows but no runs.
