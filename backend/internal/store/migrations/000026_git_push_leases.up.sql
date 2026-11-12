-- B3 (serverless epic): cross-process push leases. When the git engine
-- serves pushes from multiple serverless instances, exactly one writer may
-- finalize a repo at a time. Postgres advisory locks are session-bound and
-- push requests span processes (HTTP handler -> receive-pack), so the lease
-- is a row: acquire wins only if the row is absent or its lease expired
-- (crashed leaser); TTL-based expiry self-heals without a sweeper.

CREATE TABLE push_leases (
    repo_id uuid PRIMARY KEY REFERENCES repositories(id) ON DELETE CASCADE,
    holder text NOT NULL,
    expires timestamptz NOT NULL
);

CREATE INDEX push_leases_expiry_idx ON push_leases (expires);
