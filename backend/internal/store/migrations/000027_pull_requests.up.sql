-- B5 (serverless epic): the PR/issue model in Postgres for engine-backed
-- repos. Mirrors the Forgejo PR surface frontend-shaped: pull_requests carry
-- the (repo, number) contract; comments/reviews/line-comments attach with ON
-- DELETE CASCADE. Every row is tenant-stamped — metering and isolation never
-- join through surviving links. User identity is snapshotted (author_name)
-- so rows render without joins; IDs for the DTOs come from bigint identity
-- columns (seq), keeping the forgejo-shape int64 ids the frontend keys on.

CREATE TABLE pull_requests (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    repo_id uuid NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    tenant_id uuid NOT NULL,
    number int NOT NULL,
    title text NOT NULL,
    body text NOT NULL DEFAULT '',
    head_ref text NOT NULL,
    head_sha text NOT NULL,
    base_ref text NOT NULL,
    base_sha text NOT NULL,
    state text NOT NULL DEFAULT 'open',
    merged boolean NOT NULL DEFAULT false,
    merged_by uuid,
    merged_by_name text NOT NULL DEFAULT '',
    merged_at timestamptz,
    merge_method text NOT NULL DEFAULT '',
    merge_commit text NOT NULL DEFAULT '',
    created_by uuid,
    created_by_name text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (repo_id, number)
);

CREATE INDEX pull_requests_repo_state_idx ON pull_requests (repo_id, state);
CREATE INDEX pull_requests_tenant_idx ON pull_requests (tenant_id);

CREATE TABLE issue_comments (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    pull_id uuid NOT NULL REFERENCES pull_requests(id) ON DELETE CASCADE,
    tenant_id uuid NOT NULL,
    author_id uuid,
    author_name text NOT NULL DEFAULT '',
    body text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    seq bigint GENERATED ALWAYS AS IDENTITY UNIQUE NOT NULL
);

CREATE INDEX issue_comments_pull_idx ON issue_comments (pull_id, created_at);

CREATE TABLE pull_reviews (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    pull_id uuid NOT NULL REFERENCES pull_requests(id) ON DELETE CASCADE,
    tenant_id uuid NOT NULL,
    reviewer_id uuid,
    reviewer_name text NOT NULL DEFAULT '',
    event text NOT NULL,
    body text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    seq bigint GENERATED ALWAYS AS IDENTITY UNIQUE NOT NULL
);

CREATE INDEX pull_reviews_pull_idx ON pull_reviews (pull_id, created_at);

CREATE TABLE review_line_comments (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    pull_id uuid NOT NULL REFERENCES pull_requests(id) ON DELETE CASCADE,
    tenant_id uuid NOT NULL,
    author_id uuid,
    author_name text NOT NULL DEFAULT '',
    path text NOT NULL,
    line int NOT NULL,
    body text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    seq bigint GENERATED ALWAYS AS IDENTITY UNIQUE NOT NULL
);

CREATE INDEX review_line_comments_pull_idx ON review_line_comments (pull_id, created_at);
