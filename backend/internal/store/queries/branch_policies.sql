-- name: UpsertBranchPolicy :one
INSERT INTO branch_policies (
  repo_id, pattern, required_approvals, dismiss_stale_approvals,
  require_up_to_date, block_force_push, require_pull_request
)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (repo_id, pattern) DO UPDATE SET
  required_approvals      = EXCLUDED.required_approvals,
  dismiss_stale_approvals = EXCLUDED.dismiss_stale_approvals,
  require_up_to_date      = EXCLUDED.require_up_to_date,
  block_force_push        = EXCLUDED.block_force_push,
  require_pull_request    = EXCLUDED.require_pull_request
RETURNING *;

-- name: GetBranchPolicy :one
SELECT * FROM branch_policies
WHERE repo_id = $1 AND pattern = $2;

-- name: ListBranchPoliciesByRepo :many
SELECT * FROM branch_policies
WHERE repo_id = $1
ORDER BY pattern;

-- name: DeleteBranchPolicy :execrows
DELETE FROM branch_policies
WHERE repo_id = $1 AND pattern = $2;
