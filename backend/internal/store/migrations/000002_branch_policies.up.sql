-- Branch policies: Quill-owned protection rules for a repository's branches.
--
-- Quill is the source of truth for branch policies (Forgejo can't express the
-- platform's ownership/intent model). The platform service mirrors each policy
-- into Forgejo's branch protection so direct git pushes are blocked at the git
-- layer, while Quill additionally enforces the review gate when merging a pull
-- request through its own API.

BEGIN;

CREATE TABLE branch_policies (
  id                      uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  repo_id                 uuid NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
  -- A branch name or simple glob (e.g. 'main', 'release/*'). Branch names are
  -- case-sensitive, so the pattern is stored verbatim.
  pattern                 text NOT NULL,
  required_approvals      integer NOT NULL DEFAULT 0,
  dismiss_stale_approvals boolean NOT NULL DEFAULT false,
  require_up_to_date      boolean NOT NULL DEFAULT false,
  block_force_push        boolean NOT NULL DEFAULT true,
  require_pull_request    boolean NOT NULL DEFAULT true,
  created_at              timestamptz NOT NULL DEFAULT now(),
  updated_at              timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT branch_policies_required_approvals_nonneg CHECK (required_approvals >= 0),
  UNIQUE (repo_id, pattern)
);
CREATE INDEX branch_policies_repo_id_idx ON branch_policies (repo_id);
CREATE TRIGGER branch_policies_set_updated_at BEFORE UPDATE ON branch_policies
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();

COMMIT;
