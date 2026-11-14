-- Contribution graph (2026-11-14): the profile calendar was Forgejo's user
-- heatmap and went blank when Forgejo was switched off. The engine now
-- indexes default-branch commits (author email + date) on push, and the
-- graph counts them for the viewer's emails alongside Quill-native activity
-- (pull requests opened, reviews, issues opened).

-- One row per commit reachable from a repo's default branch (last year at
-- backfill, then everything pushed). Rows outlive force-pushes, like GitHub.
CREATE TABLE commit_activity (
  repo_id      uuid NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
  sha          text NOT NULL,
  author_email text NOT NULL,
  authored_at  timestamptz NOT NULL,
  PRIMARY KEY (repo_id, sha)
);
CREATE INDEX commit_activity_email_idx ON commit_activity (author_email, authored_at);

-- Repos whose last-year history has been indexed (push indexing walks only
-- the new range once this is set).
CREATE TABLE commit_activity_state (
  repo_id       uuid PRIMARY KEY REFERENCES repositories(id) ON DELETE CASCADE,
  backfilled_at timestamptz NOT NULL DEFAULT now()
);

-- Extra emails a user's commits are attributed to (the account email always
-- counts). Unverified: they only feed the user's own graph, and only over
-- repositories the user can read.
CREATE TABLE user_commit_emails (
  user_id    uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  email      text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (user_id, email)
);
