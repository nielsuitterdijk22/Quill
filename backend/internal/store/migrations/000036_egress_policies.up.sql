-- Pipeline egress controls (2026-11-15). An organization (tenant) policy is
-- "allow all" or an allowlist (hosts NULL = the built-in default list); each
-- repository inherits it and may add hosts of its own.
CREATE TABLE tenant_egress_policies (
    tenant_id  uuid PRIMARY KEY REFERENCES tenants(id) ON DELETE CASCADE,
    allow_all  boolean NOT NULL DEFAULT false,
    hosts      text[],
    updated_by uuid REFERENCES users(id) ON DELETE SET NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE repo_egress_hosts (
    repo_id    uuid PRIMARY KEY REFERENCES repositories(id) ON DELETE CASCADE,
    hosts      text[] NOT NULL DEFAULT '{}',
    updated_by uuid REFERENCES users(id) ON DELETE SET NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);
