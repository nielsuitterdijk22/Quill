DROP INDEX IF EXISTS git_tokens_user_forgejo_name_idx;
CREATE UNIQUE INDEX git_tokens_user_forgejo_name_idx
  ON git_tokens (user_id, forgejo_token_name);
