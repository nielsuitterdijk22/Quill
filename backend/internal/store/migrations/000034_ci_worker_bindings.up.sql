-- CI worker isolation (2026-11-14): an ephemeral CI instance serves exactly
-- one secret scope — a repository plus the deployment environment its runs
-- draw secrets from. Its first claim binds its worker id to that scope;
-- later claims only return jobs of the same scope, and unbound workers skip
-- scopes a live worker already serves. last_seen (claims, heartbeats, log
-- reports) decides liveness, so a crashed worker's scope goes to a fresh
-- instance.
ALTER TABLE pipeline_queue ADD COLUMN ci_scope text NOT NULL DEFAULT '';

CREATE TABLE ci_worker_bindings (
    worker_id text PRIMARY KEY,
    repo_id uuid NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    scope text NOT NULL,
    bound_at timestamptz NOT NULL DEFAULT now(),
    last_seen timestamptz NOT NULL DEFAULT now(),
    retired_at timestamptz
);

CREATE INDEX ci_worker_bindings_scope_idx ON ci_worker_bindings (scope, last_seen DESC);
