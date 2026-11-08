-- name: SetTenantPlan :exec
UPDATE tenants SET plan = $2 WHERE id = $1;

-- name: CountRepositoriesByTenant :one
SELECT count(*) FROM repositories r
JOIN projects p ON p.id = r.project_id
WHERE p.tenant_id = $1;

-- name: SumPipelineMinutesByTenant :one
-- Metered pipeline usage: wall-clock minutes of finished runs across all
-- pipelines of all repos in the tenant.
SELECT COALESCE(SUM(EXTRACT(EPOCH FROM (pr.finished_at - pr.started_at)) / 60), 0)::float8 AS minutes
FROM pipeline_runs pr
JOIN pipelines pl ON pl.id = pr.pipeline_id
JOIN repositories r ON r.id = pl.repo_id
JOIN projects p ON p.id = r.project_id
WHERE p.tenant_id = $1 AND pr.started_at IS NOT NULL AND pr.finished_at IS NOT NULL
  AND pr.started_at >= date_trunc('month', now());
