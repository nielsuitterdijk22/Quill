BEGIN;

CREATE TABLE repo_stars (
  user_id    uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  repo_id    uuid NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (user_id, repo_id)
);

CREATE INDEX repo_stars_repo_id_idx ON repo_stars (repo_id);

COMMIT;
