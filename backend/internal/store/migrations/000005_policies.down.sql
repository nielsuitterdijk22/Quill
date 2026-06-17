-- Revert the policy engine: restore the repo-only branch_policies table and copy
-- the repo-scoped branch policies back, then drop the unified policies table.

BEGIN;

CREATE TABLE branch_policies (
  id                      uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  repo_id                 uuid NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
  pattern                 text NOT NULL,
  required_approvals      integer NOT NULL DEFAULT 0,
  dismiss_stale_approvals boolean NOT NULL DEFAULT false,
  require_up_to_date      boolean NOT NULL DEFAULT false,
  block_force_push        boolean NOT NULL DEFAULT true,
  require_pull_request    boolean NOT NULL DEFAULT true,
  created_at              timestamptz NOT NULL DEFAULT now(),
  updated_at              timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT branch_policies_required_approvals_nonneg CHECK (required_approvals >= 0),
  UNIQUE (repo_id, pattern)
);
CREATE INDEX branch_policies_repo_id_idx ON branch_policies (repo_id);
CREATE TRIGGER branch_policies_set_updated_at BEFORE UPDATE ON branch_policies
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();

INSERT INTO branch_policies (
  repo_id, pattern, required_approvals, dismiss_stale_approvals,
  require_up_to_date, block_force_push, require_pull_request, created_at, updated_at
)
SELECT
  scope_id, selector,
  COALESCE((rules->>'requiredApprovals')::integer, 0),
  COALESCE((rules->>'dismissStaleApprovals')::boolean, false),
  COALESCE((rules->>'requireUpToDate')::boolean, false),
  COALESCE((rules->>'blockForcePush')::boolean, true),
  COALESCE((rules->>'requirePullRequest')::boolean, true),
  created_at, updated_at
FROM policies
WHERE scope_type = 'repo' AND kind = 'branch';

DROP TABLE policies;

COMMIT;
