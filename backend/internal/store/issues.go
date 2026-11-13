package store

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Issue tracker for engine-backed repos (D2 cutover). Same conventions as
// pulls.go: hand-written queries, snapshotted identity, tenant stamps.

// Issue is one issues row plus its comment count.
type Issue struct {
	ID            uuid.UUID
	RepoID        uuid.UUID
	TenantID      uuid.UUID
	Number        int
	Title         string
	Body          string
	State         string
	CreatedBy     *uuid.UUID
	CreatedByName string
	CreatedAt     time.Time
	UpdatedAt     time.Time
	ClosedAt      *time.Time
	Comments      int
}

// IssueThreadComment is one comment on an issue.
type IssueThreadComment struct {
	ID         int64 // seq: the DTO id
	IssueID    uuid.UUID
	AuthorID   *uuid.UUID
	AuthorName string
	Body       string
	CreatedAt  time.Time
}

const issueColumns = `i.id, i.repo_id, i.tenant_id, i.number, i.title, i.body, i.state,
	i.created_by, i.created_by_name, i.created_at, i.updated_at, i.closed_at,
	(SELECT count(*) FROM issue_thread_comments c WHERE c.issue_id = i.id)`

func scanIssue(row pgx.Row) (Issue, error) {
	var is Issue
	err := row.Scan(&is.ID, &is.RepoID, &is.TenantID, &is.Number, &is.Title, &is.Body, &is.State,
		&is.CreatedBy, &is.CreatedByName, &is.CreatedAt, &is.UpdatedAt, &is.ClosedAt, &is.Comments)
	return is, err
}

// lockRepoNumbers serializes per-repo number allocation across issues AND
// pull requests (one shared space) for the rest of the transaction.
func lockRepoNumbers(ctx context.Context, tx pgx.Tx, repoID uuid.UUID) error {
	_, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended('repo-numbers:' || $1::text, 0))", repoID)
	return err
}

// nextRepoNumberSQL is the next free number across both tables.
const nextRepoNumberSQL = `(SELECT GREATEST(
	(SELECT COALESCE(MAX(number), 0) FROM pull_requests WHERE repo_id = $1),
	(SELECT COALESCE(MAX(number), 0) FROM issues WHERE repo_id = $1)) + 1)`

// CreateIssue allocates the next shared per-repo number and inserts the issue.
func (s *Store) CreateIssue(ctx context.Context, repoID, tenantID uuid.UUID, createdBy *uuid.UUID, createdByName, title, body string) (Issue, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Issue{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockRepoNumbers(ctx, tx, repoID); err != nil {
		return Issue{}, err
	}
	var id uuid.UUID
	if err := tx.QueryRow(ctx, `
		INSERT INTO issues (repo_id, tenant_id, number, title, body, created_by, created_by_name)
		VALUES ($1, $2, `+nextRepoNumberSQL+`, $3, $4, $5, $6)
		RETURNING id`, repoID, tenantID, title, body, createdBy, createdByName).Scan(&id); err != nil {
		return Issue{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Issue{}, err
	}
	return s.getIssue(ctx, "i.id = $1", id)
}

func (s *Store) getIssue(ctx context.Context, where string, args ...any) (Issue, error) {
	is, err := scanIssue(s.pool.QueryRow(ctx, "SELECT "+issueColumns+" FROM issues i WHERE "+where, args...))
	if errors.Is(err, pgx.ErrNoRows) {
		return Issue{}, pgx.ErrNoRows
	}
	return is, err
}

// GetIssueByNumber loads one issue by its per-repo number.
func (s *Store) GetIssueByNumber(ctx context.Context, repoID uuid.UUID, number int) (Issue, error) {
	return s.getIssue(ctx, "i.repo_id = $1 AND i.number = $2", repoID, number)
}

// ListIssuesByRepo pages a repo's issues ("open" | "closed" | "all"), newest first.
func (s *Store) ListIssuesByRepo(ctx context.Context, repoID uuid.UUID, state string, page, pageSize int) ([]Issue, error) {
	if page < 1 {
		page = 1
	}
	q := "SELECT " + issueColumns + " FROM issues i WHERE i.repo_id = $1"
	args := []any{repoID}
	if state == "open" || state == "closed" {
		q += " AND i.state = $2"
		args = append(args, state)
	}
	q += " ORDER BY i.number DESC"
	args = append(args, pageSize, (page-1)*pageSize)
	q += " LIMIT $" + strconv.Itoa(len(args)-1) + " OFFSET $" + strconv.Itoa(len(args))
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Issue
	for rows.Next() {
		is, err := scanIssue(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, is)
	}
	return out, rows.Err()
}

// UpdateIssue patches title and/or state (nil = unchanged).
func (s *Store) UpdateIssue(ctx context.Context, id uuid.UUID, title, state *string) (Issue, error) {
	if _, err := s.pool.Exec(ctx, `
		UPDATE issues SET
			title = COALESCE($2, title),
			state = COALESCE($3, state),
			closed_at = CASE
				WHEN $3::text = 'closed' AND state <> 'closed' THEN now()
				WHEN $3::text = 'open' THEN NULL
				ELSE closed_at END,
			updated_at = now()
		WHERE id = $1`, id, title, state); err != nil {
		return Issue{}, err
	}
	return s.getIssue(ctx, "i.id = $1", id)
}

// CreateIssueThreadComment appends a comment and bumps the issue's updated_at.
func (s *Store) CreateIssueThreadComment(ctx context.Context, issueID, tenantID uuid.UUID, authorID *uuid.UUID, authorName, body string) (IssueThreadComment, error) {
	c := IssueThreadComment{IssueID: issueID, AuthorID: authorID, AuthorName: authorName, Body: body}
	if err := s.pool.QueryRow(ctx, `
		INSERT INTO issue_thread_comments (issue_id, tenant_id, author_id, author_name, body)
		VALUES ($1, $2, $3, $4, $5) RETURNING seq, created_at`,
		issueID, tenantID, authorID, authorName, body).Scan(&c.ID, &c.CreatedAt); err != nil {
		return IssueThreadComment{}, err
	}
	_, _ = s.pool.Exec(ctx, "UPDATE issues SET updated_at = now() WHERE id = $1", issueID)
	return c, nil
}

// ListIssueThreadComments returns an issue's comments, oldest first.
func (s *Store) ListIssueThreadComments(ctx context.Context, issueID uuid.UUID) ([]IssueThreadComment, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT seq, issue_id, author_id, author_name, body, created_at
		FROM issue_thread_comments WHERE issue_id = $1 ORDER BY created_at, seq`, issueID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []IssueThreadComment
	for rows.Next() {
		var c IssueThreadComment
		if err := rows.Scan(&c.ID, &c.IssueID, &c.AuthorID, &c.AuthorName, &c.Body, &c.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
