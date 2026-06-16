-- Git access tokens: platform-side metadata for the personal access tokens Quill
-- mints in Forgejo for git-over-HTTPS. Forgejo stores the token secret; Quill
-- records just enough to list and revoke them (the secret is shown once and
-- never persisted).

BEGIN;

CREATE TABLE git_tokens (
  id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id            uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  -- Human-friendly label the user chose for this token.
  name               text NOT NULL,
  -- The token's name in Forgejo (unique per Forgejo user), used to revoke it.
  forgejo_token_name text NOT NULL,
  created_at         timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX git_tokens_user_id_idx ON git_tokens (user_id);
CREATE UNIQUE INDEX git_tokens_user_forgejo_name_idx
  ON git_tokens (user_id, forgejo_token_name);

COMMIT;
