package store

import (
	"context"

	"github.com/google/uuid"
)

// LiveLogLine is one buffered output line of an in-flight pipeline run.
type LiveLogLine struct {
	ID       int64
	JobKey   string
	StepName string
	Line     string
}

// AppendLiveLogs inserts lines for runID in order (one round-trip).
func (s *Store) AppendLiveLogs(ctx context.Context, runID uuid.UUID, lines []LiveLogLine) error {
	if len(lines) == 0 {
		return nil
	}
	jobs := make([]string, len(lines))
	steps := make([]string, len(lines))
	texts := make([]string, len(lines))
	for i, l := range lines {
		jobs[i], steps[i], texts[i] = l.JobKey, l.StepName, l.Line
	}
	const q = `
		INSERT INTO pipeline_live_logs (run_id, job_key, step_name, line)
		SELECT $1, j, s, l FROM unnest($2::text[], $3::text[], $4::text[]) WITH ORDINALITY AS t(j, s, l, n)
		ORDER BY n`
	_, err := s.pool.Exec(ctx, q, runID, jobs, steps, texts)
	return err
}

// ListLiveLogs returns up to limit lines of runID with id > after, oldest first.
func (s *Store) ListLiveLogs(ctx context.Context, runID uuid.UUID, after int64, limit int) ([]LiveLogLine, error) {
	const q = `
		SELECT id, job_key, step_name, line FROM pipeline_live_logs
		WHERE run_id = $1 AND id > $2 ORDER BY id LIMIT $3`
	rows, err := s.pool.Query(ctx, q, runID, after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LiveLogLine
	for rows.Next() {
		var l LiveLogLine
		if err := rows.Scan(&l.ID, &l.JobKey, &l.StepName, &l.Line); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// DeleteLiveLogs drops runID's buffered lines (the final result is persisted).
func (s *Store) DeleteLiveLogs(ctx context.Context, runID uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM pipeline_live_logs WHERE run_id = $1`, runID)
	return err
}
