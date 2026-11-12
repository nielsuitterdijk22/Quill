-- B6 (serverless epic): git endpoint authentication + tenant push metering.
-- git_tokens gains a token_hash so Quill-native git tokens (qgt_...) validate
-- locally by hash lookup: no per-push round-trip to a scale-to-zero IdP.
-- Legacy Forgejo-backed rows keep token_hash empty. git_push_meters counts
-- pushes per tenant per day (metering never depends on joins).

ALTER TABLE git_tokens ADD COLUMN token_hash text NOT NULL DEFAULT '';

CREATE INDEX git_tokens_hash_idx ON git_tokens (token_hash) WHERE token_hash <> '';

CREATE TABLE git_push_meters (
    tenant_id uuid NOT NULL,
    day date NOT NULL DEFAULT current_date,
    pushes int NOT NULL DEFAULT 0,
    PRIMARY KEY (tenant_id, day)
);
