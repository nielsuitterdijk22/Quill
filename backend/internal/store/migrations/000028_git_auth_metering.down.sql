DROP TABLE git_push_meters;
DROP INDEX IF EXISTS git_tokens_hash_idx;
ALTER TABLE git_tokens DROP COLUMN token_hash;
