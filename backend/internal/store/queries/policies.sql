-- name: ListPoliciesByScope :many
-- Policies declared directly at one scope for a kind (e.g. a repo's own branch
-- policies). Used to render and edit what a scope owns, before inheritance.
SELECT * FROM policies
WHERE scope_type = $1 AND scope_id = $2 AND kind = $3
ORDER BY selector;

-- name: ListEffectivePolicies :many
-- Every enabled policy of a kind across the scopes governing a repo: the repo
-- itself, its project, and that project's tenant. Ordered broad -> narrow so the
-- resolver can fold tenant onto project onto repo.
SELECT * FROM policies
WHERE enabled
  AND kind = $1
  AND (
    (scope_type = 'repo' AND scope_id = $2)
    OR (scope_type = 'project' AND scope_id = $3)
    OR (scope_type = 'tenant' AND scope_id = $4)
  )
ORDER BY CASE scope_type WHEN 'tenant' THEN 0 WHEN 'project' THEN 1 ELSE 2 END, selector;

-- name: UpsertPolicy :one
INSERT INTO policies (scope_type, scope_id, kind, selector, rules, locked, enabled)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (scope_type, scope_id, kind, selector) DO UPDATE SET
  rules   = EXCLUDED.rules,
  locked  = EXCLUDED.locked,
  enabled = EXCLUDED.enabled
RETURNING *;

-- name: DeletePolicy :execrows
DELETE FROM policies
WHERE scope_type = $1 AND scope_id = $2 AND kind = $3 AND selector = $4;

-- name: DeletePoliciesByScope :execrows
-- Remove all policies attached to a scope (used when the scope is deleted, since
-- scope_id is polymorphic and cannot cascade via a foreign key).
DELETE FROM policies
WHERE scope_type = $1 AND scope_id = $2;
