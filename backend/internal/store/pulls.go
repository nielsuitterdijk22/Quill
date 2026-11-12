package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// PR/issue model for engine-backed repos (serverless epic, PR B5). Hand-
// written queries in the git_state.go tradition: rows mirror the forgejo PR
// surface the platform layer maps onto the existing DTOs (frontend
// unchanged). Identity is snapshotted (author/merger names); seq identity
// columns back the DTO int64 IDs.

// PullRequest is one pull_requests row.
type PullRequest struct {
	ID            uuid.UUID
	RepoID        uuid.UUID
	TenantID      uuid.UUID
	Number        int
	Title         string
	Body          string
	HeadRef       string
	HeadSHA       string
	BaseRef       string
	BaseSHA       string
	State         string
	Merged        bool
	MergedBy      *uuid.UUID
	MergedByName  string
	MergedAt      *time.Time
	MergeMethod   string
	MergeCommit   string
	CreatedBy     *uuid.UUID
	CreatedByName string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

const pullColumns = `id, repo_id, tenant_id, number, title, body, head_ref, head_sha,
	base_ref, base_sha, state, merged, merged_by, merged_by_name, merged_at,
	merge_method, merge_commit, created_by, created_by_name, created_at, updated_at`

// scanPull decodes one pull_requests row.
func scanPull(row pgx.Row) (PullRequest, error) {
	var pr PullRequest
	err := row.Scan(&pr.ID, &pr.RepoID, &pr.TenantID, &pr.Number, &pr.Title, &pr.Body,
		&pr.HeadRef, &pr.HeadSHA, &pr.BaseRef, &pr.BaseSHA, &pr.State, &pr.Merged,
		&pr.MergedBy, &pr.MergedByName, &pr.MergedAt, &pr.MergeMethod, &pr.MergeCommit,
		&pr.CreatedBy, &pr.CreatedByName, &pr.CreatedAt, &pr.UpdatedAt)
	return pr, err
}

// ErrPullNumberTaken means a concurrent PR creation raced the per-repo number
// allocation: the caller retries (bounded) or fails with a conflict.
var ErrPullNumberTaken = errors.New("pull request number raced")

// CreatePull allocates the next per-repo number and inserts the row atomically
// (the UNIQUE (repo_id, number) constraint is the arbiter).
func (s *Store) CreatePull(ctx context.Context, repoID, tenantID, createdBy uuid.UUID, createdByLogin, title, body, headRef, headSHA, baseRef, baseSHA string) (PullRequest, error) {
	const q = `
		INSERT INTO pull_requests
			(repo_id, tenant_id, number, title, body, head_ref, head_sha, base_ref, base_sha,
			 created_by, created_by_name)
		VALUES ($1, $2,
			(SELECT COALESCE(MAX(number), 0) + 1 FROM pull_requests WHERE repo_id = $1),
			$3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING ` + pullColumns
	row := s.pool.QueryRow(ctx, q, repoID, tenantID,
		title, body, headRef, headSHA, baseRef, baseSHA, createdBy, createdByLogin)
	pr, err := scanPull(row)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return PullRequest{}, ErrPullNumberTaken
	}
	if err != nil {
		return PullRequest{}, err
	}
	return pr, nil
}

// GetPullByNumber loads one pull request by its per-repo number.
func (s *Store) GetPullByNumber(ctx context.Context, repoID uuid.UUID, number int) (PullRequest, error) {
	const q = "SELECT " + pullColumns + " FROM pull_requests WHERE repo_id = $1 AND number = $2"
	pr, err := scanPull(s.pool.QueryRow(ctx, q, repoID, number))
	if errors.Is(err, pgx.ErrNoRows) {
		return PullRequest{}, pgx.ErrNoRows
	}
	return pr, err
}

// ListPullsByRepo returns a repo's pulls filtered by "open", "closed", or "all".
func (s *Store) ListPullsByRepo(ctx context.Context, repoID uuid.UUID, state string) ([]PullRequest, error) {
	q := "SELECT " + pullColumns + " FROM pull_requests WHERE repo_id = $1"
	args := []any{repoID}
	if state == "open" || state == "closed" {
		q += " AND state = $2"
		args = append(args, state)
	}
	q += " ORDER BY number DESC LIMIT 100"
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PullRequest
	for rows.Next() {
		pr, err := scanPull(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, pr)
	}
	return out, rows.Err()
}

// PullCommentCount returns the conversation comment count for one pull.
func (s *Store) PullCommentCount(ctx context.Context, pullID uuid.UUID) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx,
		"SELECT count(*) FROM issue_comments WHERE pull_id = $1", pullID).Scan(&n)
	return n, err
}

// IssueCommentRow is one conversation comment.
type IssueCommentRow struct {
	ID         int64
	Body       string
	AuthorName string
	CreatedAt  time.Time
}

// ListPullComments returns a pull's conversation comments, oldest first.
func (s *Store) ListPullComments(ctx context.Context, pullID uuid.UUID) ([]IssueCommentRow, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT seq, body, author_name, created_at FROM issue_comments
		WHERE pull_id = $1 ORDER BY created_at, seq`, pullID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []IssueCommentRow
	for rows.Next() {
		var c IssueCommentRow
		if err := rows.Scan(&c.ID, &c.Body, &c.AuthorName, &c.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// InsertPullComment appends a conversation comment attributed to userID.
func (s *Store) InsertPullComment(ctx context.Context, pullID, tenantID, authorID uuid.UUID, authorLogin, body string) (IssueCommentRow, error) {
	var c IssueCommentRow
	err := s.pool.QueryRow(ctx, `
		INSERT INTO issue_comments (pull_id, tenant_id, author_id, author_name, body)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING seq, body, author_name, created_at`,
		pullID, tenantID, authorID, authorLogin, body).
		Scan(&c.ID, &c.Body, &c.AuthorName, &c.CreatedAt)
	return c, err
}

// ReviewRow is one pull review (APPROVED | REQUEST_CHANGES | COMMENT).
type ReviewRow struct {
	ID           int64
	ReviewerName string
	Event        string
	Body         string
	CreatedAt    time.Time
}

// ListPullReviews returns a pull's reviews, oldest first (submission order —
// the gate's latest-per-user logic depends on it).
func (s *Store) ListPullReviews(ctx context.Context, pullID uuid.UUID) ([]ReviewRow, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT seq, reviewer_name, event, body, created_at FROM pull_reviews
		WHERE pull_id = $1 ORDER BY created_at, seq`, pullID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ReviewRow
	for rows.Next() {
		var r ReviewRow
		if err := rows.Scan(&r.ID, &r.ReviewerName, &r.Event, &r.Body, &r.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// InsertPullReview records one review event attributed to userID.
func (s *Store) InsertPullReview(ctx context.Context, pullID, tenantID, reviewerID uuid.UUID, reviewerLogin, event, body string) (ReviewRow, error) {
	var r ReviewRow
	err := s.pool.QueryRow(ctx, `
		INSERT INTO pull_reviews (pull_id, tenant_id, reviewer_id, reviewer_name, event, body)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING seq, reviewer_name, event, body, created_at`,
		pullID, tenantID, reviewerID, reviewerLogin, event, body).
		Scan(&r.ID, &r.ReviewerName, &r.Event, &r.Body, &r.CreatedAt)
	return r, err
}

// LineCommentRow is one line-anchored review comment.
type LineCommentRow struct {
	ID         int64
	Path       string
	Line       int
	Body       string
	AuthorName string
	CreatedAt  time.Time
}

// ListPullLineComments returns a pull's line comments, oldest first.
func (s *Store) ListPullLineComments(ctx context.Context, pullID uuid.UUID) ([]LineCommentRow, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT seq, path, line, body, author_name, created_at FROM review_line_comments
		WHERE pull_id = $1 ORDER BY created_at, seq`, pullID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LineCommentRow
	for rows.Next() {
		var c LineCommentRow
		if err := rows.Scan(&c.ID, &c.Path, &c.Line, &c.Body, &c.AuthorName, &c.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// InsertPullLineComment posts one line-anchored comment attributed to userID.
func (s *Store) InsertPullLineComment(ctx context.Context, pullID, tenantID, authorID uuid.UUID, authorLogin, path string, line int, body string) (LineCommentRow, error) {
	var c LineCommentRow
	err := s.pool.QueryRow(ctx, `
		INSERT INTO review_line_comments (pull_id, tenant_id, author_id, author_name, path, line, body)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING seq, path, line, body, author_name, created_at`,
		pullID, tenantID, authorID, authorLogin, path, line, body).
		Scan(&c.ID, &c.Path, &c.Line, &c.Body, &c.AuthorName, &c.CreatedAt)
	return c, err
}

// MarkPullMerged closes and stamps a pull as merged.
func (s *Store) MarkPullMerged(ctx context.Context, repoID uuid.UUID, number int, mergedBy uuid.UUID, mergedByLogin, method, mergeCommit string) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE pull_requests
		SET state = 'closed', merged = true, merged_by = $3, merged_by_name = $4,
			merged_at = now(), merge_method = $5, merge_commit = $6, updated_at = now()
		WHERE repo_id = $1 AND number = $2 AND merged = false`,
		repoID, number, mergedBy, mergedByLogin, method, mergeCommit)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return errors.New("pull request already merged or closed")
	}
	return nil
}

// EnginePullListing is one engine pull flattened for the cross-repo overview.
type EnginePullListing struct {
	ProjectSlug string
	RepoSlug    string
	RepoName    string
	Pull        PullRequest
}

// ListEnginePullsByProjects returns engine pull requests across the given
// projects (state filter "open"/"closed"/"" for all), newest-updated first.
func (s *Store) ListEnginePullsByProjects(ctx context.Context, projectIDs []uuid.UUID, state string) ([]EnginePullListing, error) {
	if len(projectIDs) == 0 {
		return nil, nil
	}
	q := `
		SELECT p.slug, r.slug, r.name,
			pr.id, pr.repo_id, pr.tenant_id, pr.number, pr.title, pr.body,
			pr.head_ref, pr.head_sha, pr.base_ref, pr.base_sha, pr.state, pr.merged,
			pr.merged_by, pr.merged_by_name, pr.merged_at, pr.merge_method, pr.merge_commit,
			pr.created_by, pr.created_by_name, pr.created_at, pr.updated_at
		FROM pull_requests pr
		JOIN repositories r ON r.id = pr.repo_id
		JOIN projects p ON p.id = r.project_id
		WHERE r.project_id = ANY($1)`
	args := []any{projectIDs}
	if state == "open" || state == "closed" {
		q += " AND pr.state = $2"
		args = append(args, state)
	}
	q += " ORDER BY pr.updated_at DESC LIMIT 500"
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []EnginePullListing
	for rows.Next() {
		var l EnginePullListing
		err := rows.Scan(&l.ProjectSlug, &l.RepoSlug, &l.RepoName,
			&l.Pull.ID, &l.Pull.RepoID, &l.Pull.TenantID, &l.Pull.Number, &l.Pull.Title, &l.Pull.Body,
			&l.Pull.HeadRef, &l.Pull.HeadSHA, &l.Pull.BaseRef, &l.Pull.BaseSHA, &l.Pull.State, &l.Pull.Merged,
			&l.Pull.MergedBy, &l.Pull.MergedByName, &l.Pull.MergedAt, &l.Pull.MergeMethod, &l.Pull.MergeCommit,
			&l.Pull.CreatedBy, &l.Pull.CreatedByName, &l.Pull.CreatedAt, &l.Pull.UpdatedAt)
		if err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// CountEngineOpenPulls counts open engine pull requests across projects.
func (s *Store) CountEngineOpenPulls(ctx context.Context, projectIDs []uuid.UUID) (int, error) {
	if len(projectIDs) == 0 {
		return 0, nil
	}
	var n int
	err := s.pool.QueryRow(ctx, `
		SELECT count(*) FROM pull_requests pr
		JOIN repositories r ON r.id = pr.repo_id
		WHERE r.project_id = ANY($1) AND pr.state = 'open'`, projectIDs).Scan(&n)
	return n, err
}

// TouchPull refreshes a pull's updated_at (conversation/review activity).
func (s *Store) TouchPull(ctx context.Context, pullID uuid.UUID) error {
	_, err := s.pool.Exec(ctx,
		"UPDATE pull_requests SET updated_at = now() WHERE id = $1", pullID)
	return err
}
