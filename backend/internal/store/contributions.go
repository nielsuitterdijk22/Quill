package store

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// CommitActivity is one indexed default-branch commit (migration 000035).
type CommitActivity struct {
	SHA         string
	AuthorEmail string
	AuthoredAt  time.Time
}

// ContributionDay is one UTC day's contribution count.
type ContributionDay struct {
	Day   time.Time
	Count int
}

// InsertCommitActivity records commits of a repo; already-known shas are
// skipped, so re-indexing a range is harmless.
func (s *Store) InsertCommitActivity(ctx context.Context, repoID uuid.UUID, commits []CommitActivity) error {
	const batch = 1000
	for len(commits) > 0 {
		n := min(batch, len(commits))
		shas := make([]string, n)
		emails := make([]string, n)
		ats := make([]time.Time, n)
		for i, c := range commits[:n] {
			shas[i], emails[i], ats[i] = c.SHA, c.AuthorEmail, c.AuthoredAt
		}
		if _, err := s.pool.Exec(ctx, `
			INSERT INTO commit_activity (repo_id, sha, author_email, authored_at)
			SELECT $1, u.sha, lower(u.email), u.at
			FROM unnest($2::text[], $3::text[], $4::timestamptz[]) AS u(sha, email, at)
			ON CONFLICT DO NOTHING`, repoID, shas, emails, ats); err != nil {
			return err
		}
		commits = commits[n:]
	}
	return nil
}

// CommitActivityBackfilled reports whether a repo's last-year history has
// been indexed.
func (s *Store) CommitActivityBackfilled(ctx context.Context, repoID uuid.UUID) (bool, error) {
	var ok bool
	err := s.pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM commit_activity_state WHERE repo_id = $1)`, repoID).Scan(&ok)
	return ok, err
}

// MarkCommitActivityBackfilled records that a repo's history is indexed.
func (s *Store) MarkCommitActivityBackfilled(ctx context.Context, repoID uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO commit_activity_state (repo_id) VALUES ($1)
		ON CONFLICT (repo_id) DO UPDATE SET backfilled_at = now()`, repoID)
	return err
}

// UserCommitEmails lists the extra commit emails of a user (lowercased).
func (s *Store) UserCommitEmails(ctx context.Context, userID uuid.UUID) ([]string, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT email FROM user_commit_emails WHERE user_id = $1 ORDER BY created_at, email`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var e string
		if err := rows.Scan(&e); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// SetUserCommitEmails replaces a user's extra commit emails. Callers
// normalise (lowercase, dedupe) first.
func (s *Store) SetUserCommitEmails(ctx context.Context, userID uuid.UUID, emails []string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op after commit
	if _, err := tx.Exec(ctx, `DELETE FROM user_commit_emails WHERE user_id = $1`, userID); err != nil {
		return err
	}
	if len(emails) > 0 {
		if _, err := tx.Exec(ctx, `
			INSERT INTO user_commit_emails (user_id, email)
			SELECT $1, e FROM unnest($2::text[]) AS e
			ON CONFLICT DO NOTHING`, userID, emails); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// UserContributions counts a user's contributions per UTC day since `since`,
// over repositories of the given projects only (the caller passes the
// projects the user can see — commit emails are unverified, so attribution
// must never reach repos the user cannot read). A contribution is a commit
// authored with one of `emails` (each sha once, however many forks hold it),
// a pull request opened, a review submitted, or an issue opened.
func (s *Store) UserContributions(ctx context.Context, userID uuid.UUID, emails []string, projectIDs []uuid.UUID, since time.Time) ([]ContributionDay, error) {
	if len(projectIDs) == 0 {
		return []ContributionDay{}, nil
	}
	rows, err := s.pool.Query(ctx, `
		WITH repos AS (SELECT id FROM repositories WHERE project_id = ANY($3)),
		ev AS (
			SELECT min(c.authored_at) AS at
			FROM commit_activity c
			WHERE c.repo_id IN (SELECT id FROM repos)
			  AND c.author_email = ANY($2) AND c.authored_at >= $4
			GROUP BY c.sha
			UNION ALL
			SELECT p.created_at FROM pull_requests p
			WHERE p.created_by = $1 AND p.created_at >= $4 AND p.repo_id IN (SELECT id FROM repos)
			UNION ALL
			SELECT pr.created_at FROM pull_reviews pr
			JOIN pull_requests p ON p.id = pr.pull_id
			WHERE pr.reviewer_id = $1 AND pr.created_at >= $4 AND p.repo_id IN (SELECT id FROM repos)
			UNION ALL
			SELECT i.created_at FROM issues i
			WHERE i.created_by = $1 AND i.created_at >= $4 AND i.repo_id IN (SELECT id FROM repos)
		)
		SELECT date_trunc('day', at AT TIME ZONE 'UTC') AS day, count(*)
		FROM ev GROUP BY 1 ORDER BY 1`, userID, emails, projectIDs, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ContributionDay{}
	for rows.Next() {
		var d ContributionDay
		if err := rows.Scan(&d.Day, &d.Count); err != nil {
			return nil, err
		}
		d.Day = time.Date(d.Day.Year(), d.Day.Month(), d.Day.Day(), 0, 0, 0, 0, time.UTC)
		out = append(out, d)
	}
	return out, rows.Err()
}
