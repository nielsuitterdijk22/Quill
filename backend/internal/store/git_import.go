package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Forgejo → engine import surface (serverless epic, PR D1). Hand-written in
// the git_state.go tradition: the migration tool inserts rows that PRESERVE
// Forgejo history (original numbers, authors, timestamps), which the ordinary
// creation paths (CreatePull and friends) never accept — numbers are engine-
// allocated there, not carried. Idempotent: re-running an import is a no-op
// for rows already present (UNIQUE (repo_id, number) is the arbiter), so a
// partially-finished migration resumes cleanly.

// ForgejoRepoLink is one repository with its Forgejo mirror identity — the
// enumeration the migration tool walks. TenantID may be nil for legacy rows
// (the referee stamp is nullable for exactly this reason); such repos cannot
// migrate into the tenant-scoped bucket layout and are reported, not
// migrated.
type ForgejoRepoLink struct {
	RepoID         uuid.UUID
	RepoSlug       string
	ProjectID      uuid.UUID
	ProjectSlug    string
	TenantID       *uuid.UUID
	ForgejoOwner   string
	ForgejoName    string
	ForgejoRepoID  *int64
	Visibility     string
	IsArchived     bool
	DefaultBranch  string
	HasEngineState bool
}

// ListForgejoRepoLinks enumerates every non-archived repository that carries a
// Forgejo mirror link, with a flag for whether the engine already owns it (a
// git_repo_state row). Ordered for stable resume runs.
func (s *Store) ListForgejoRepoLinks(ctx context.Context) ([]ForgejoRepoLink, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT r.id, r.slug, p.id, p.slug, p.tenant_id,
		       COALESCE(r.forgejo_owner, ''), COALESCE(r.forgejo_name, ''),
		       r.forgejo_repo_id, r.visibility, r.is_archived,
		       COALESCE(r.default_branch, 'main'),
		       EXISTS (SELECT 1 FROM git_repo_state g WHERE g.repo_id = r.id)
		FROM repositories r
		JOIN projects p ON p.id = r.project_id
		WHERE r.forgejo_repo_id IS NOT NULL AND NOT r.is_archived
		ORDER BY p.slug, r.slug`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ForgejoRepoLink
	for rows.Next() {
		var l ForgejoRepoLink
		var owner, name string
		if err := rows.Scan(&l.RepoID, &l.RepoSlug, &l.ProjectID, &l.ProjectSlug, &l.TenantID,
			&owner, &name, &l.ForgejoRepoID, &l.Visibility, &l.IsArchived,
			&l.DefaultBranch, &l.HasEngineState); err != nil {
			return nil, err
		}
		l.ForgejoOwner, l.ForgejoName = owner, name
		out = append(out, l)
	}
	return out, rows.Err()
}

// ImportPull inserts a pull_requests row with an EXPLICIT number and
// timestamps (Forgejo history); unlike CreatePull it does not allocate. A
// row that already exists (same repo + number) is left untouched and the
// stored row is returned, making re-runs safe.
func (s *Store) ImportPull(ctx context.Context, repoID, tenantID uuid.UUID, number int,
	title, body, headRef, headSHA, baseRef, baseSHA, state string,
	merged bool, mergedBy *uuid.UUID, mergedByName string, mergedAt *time.Time, mergeCommit string,
	createdBy *uuid.UUID, createdByName string, createdAt time.Time) (PullRequest, error) {
	const q = `
		INSERT INTO pull_requests
			(repo_id, tenant_id, number, title, body, head_ref, head_sha,
			 base_ref, base_sha, state, merged, merged_by, merged_by_name,
			 merged_at, merge_commit, created_by, created_by_name, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $18)
		ON CONFLICT (repo_id, number) DO NOTHING
		RETURNING ` + pullColumns
	row := s.pool.QueryRow(ctx, q, repoID, tenantID, number, title, body, headRef, headSHA,
		baseRef, baseSHA, state, merged, mergedBy, mergedByName, mergedAt, mergeCommit,
		createdBy, createdByName, createdAt)
	pr, err := scanPull(row)
	if errors.Is(err, pgx.ErrNoRows) {
		// Already imported on a previous run: return the stored row.
		return s.GetPullByNumber(ctx, repoID, number)
	}
	return pr, err
}

// ImportPullComment inserts a conversation comment with its original
// timestamp and author snapshot (chronological order preserves the seq order).
func (s *Store) ImportPullComment(ctx context.Context, pullID, tenantID uuid.UUID,
	authorID *uuid.UUID, authorName, body string, createdAt time.Time) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO issue_comments (pull_id, tenant_id, author_id, author_name, body, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)`, pullID, tenantID, authorID, authorName, body, createdAt)
	return err
}

// ImportPullReview inserts a review row with its original timestamp. The
// event carries Forgejo's vocabulary verbatim (APPROVED / REQUEST_CHANGES /
// COMMENT), exactly what engine_pulls stores on live reviews.
func (s *Store) ImportPullReview(ctx context.Context, pullID, tenantID uuid.UUID,
	reviewerID *uuid.UUID, reviewerName, event, body string, createdAt time.Time) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO pull_reviews (pull_id, tenant_id, reviewer_id, reviewer_name, event, body, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`, pullID, tenantID, reviewerID, reviewerName, event, body, createdAt)
	return err
}

// ImportPullLineComment inserts a review-anchored line comment with its
// original timestamp.
func (s *Store) ImportPullLineComment(ctx context.Context, pullID, tenantID uuid.UUID,
	authorID *uuid.UUID, authorName, path string, line int, body string, createdAt time.Time) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO review_line_comments (pull_id, tenant_id, author_id, author_name, path, line, body, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`, pullID, tenantID, authorID, authorName, path, line, body, createdAt)
	return err
}

// FindUserForForgejoLogin resolves a Forgejo login to a Quill user: by the
// explicit forgejo_username link first, then by username equality (legacy
// accounts were provisioned with matching usernames). Not found = nil uuid
// with no error — history import NEVER blocks on identity resolution; the
// row keeps its author-name snapshot and a NULL author id.
func (s *Store) FindUserForForgejoLogin(ctx context.Context, login string) (uuid.UUID, bool, error) {
	if login == "" {
		return uuid.Nil, false, nil
	}
	var id uuid.UUID
	err := s.pool.QueryRow(ctx, `
		SELECT id FROM users
		WHERE lower(COALESCE(forgejo_username, '')) = lower($1)
		   OR lower(username) = lower($1)
		LIMIT 1`, login).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, false, nil
	}
	return id, err == nil, err
}
