-- Policy engine: a single, scope-attached policy store that supersedes the
-- repo-only branch_policies table.
--
-- Quill governs repositories through policies declared at three scopes —
-- tenant, project, and repo — and resolves the effective rule for a target by
-- merging from the broadest scope inward (tenant -> project -> repo). A broader
-- scope may set `locked` to forbid narrower scopes from weakening it; narrower
-- scopes may then only tighten. The `kind` discriminates the policy domain
-- (branch protection today; environment and artefact-promotion gates later),
-- and `rules` carries the kind-specific settings as JSON so new kinds don't need
-- schema changes. `selector` targets within a kind (a branch glob, an
-- environment name, an artefact/channel pattern).

BEGIN;

CREATE TABLE policies (
  id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  -- The scope a policy attaches to. scope_id references tenants/projects/
  -- repositories depending on scope_type; it is polymorphic, so no single FK is
  -- possible — the platform service cleans up rows when a scope is deleted.
  scope_type text NOT NULL,
  scope_id   uuid NOT NULL,
  -- Policy domain: 'branch' | 'environment' | 'artifact_promotion'.
  kind       text NOT NULL,
  -- Target within the kind (e.g. branch name/glob 'main', 'release/*').
  selector   text NOT NULL,
  -- Kind-specific settings (e.g. requiredApprovals, requirePullRequest).
  rules      jsonb NOT NULL DEFAULT '{}'::jsonb,
  -- When true, narrower scopes may only tighten this policy, never weaken it.
  locked     boolean NOT NULL DEFAULT false,
  enabled    boolean NOT NULL DEFAULT true,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT policies_scope_type_check CHECK (scope_type IN ('tenant', 'project', 'repo')),
  UNIQUE (scope_type, scope_id, kind, selector)
);
CREATE INDEX policies_scope_idx ON policies (scope_type, scope_id, kind);
CREATE TRIGGER policies_set_updated_at BEFORE UPDATE ON policies
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- Backfill the existing repo-scoped branch policies into the unified table.
INSERT INTO policies (scope_type, scope_id, kind, selector, rules, created_at, updated_at)
SELECT
  'repo', repo_id, 'branch', pattern,
  jsonb_build_object(
    'requiredApprovals', required_approvals,
    'dismissStaleApprovals', dismiss_stale_approvals,
    'requireUpToDate', require_up_to_date,
    'blockForcePush', block_force_push,
    'requirePullRequest', require_pull_request
  ),
  created_at, updated_at
FROM branch_policies;

DROP TABLE branch_policies;

COMMIT;
