-- D2 cutover: the issue tracker for engine-backed repos (Forgejo owned issues
-- until now; every migrated/new repo 502'd on create). Mirrors the B5 PR
-- model: tenant-stamped rows, snapshotted author names, seq identities for
-- the forgejo-shape int64 comment ids. Numbers share the per-repo space with
-- pull_requests (allocated under one advisory lock, see store/issues.go).

CREATE TABLE issues (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    repo_id uuid NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    tenant_id uuid NOT NULL,
    number int NOT NULL,
    title text NOT NULL,
    body text NOT NULL DEFAULT '',
    state text NOT NULL DEFAULT 'open',
    created_by uuid,
    created_by_name text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    closed_at timestamptz,
    UNIQUE (repo_id, number)
);

CREATE INDEX issues_repo_state_idx ON issues (repo_id, state);
CREATE INDEX issues_tenant_idx ON issues (tenant_id);

CREATE TABLE issue_thread_comments (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    issue_id uuid NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
    tenant_id uuid NOT NULL,
    author_id uuid,
    author_name text NOT NULL DEFAULT '',
    body text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    seq bigint GENERATED ALWAYS AS IDENTITY UNIQUE NOT NULL
);

CREATE INDEX issue_thread_comments_issue_idx ON issue_thread_comments (issue_id, created_at);
