-- D2 cutover: native qgt_ tokens store an empty forgejo_token_name, so the
-- Forgejo-era unique index made a user's SECOND native token collide. Keep
-- the uniqueness rule for legacy Forgejo-backed rows only (their name is what
-- revocation looks up); native tokens are identified by token_hash.
DROP INDEX IF EXISTS git_tokens_user_forgejo_name_idx;
CREATE UNIQUE INDEX git_tokens_user_forgejo_name_idx
  ON git_tokens (user_id, forgejo_token_name) WHERE forgejo_token_name <> '';
