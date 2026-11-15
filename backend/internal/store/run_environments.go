package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// RunEnvironment is the deploy target of a run (migration 000037).
type RunEnvironment struct {
	Environment   string
	BlockedReason string
}

// SetRunEnvironment records a run's target environment and, for a run the
// gate refused, why.
func (s *Store) SetRunEnvironment(ctx context.Context, runID uuid.UUID, envID uuid.NullUUID, env, blocked string) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO pipeline_run_environments (run_id, environment_id, environment, blocked_reason)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (run_id) DO UPDATE SET environment_id = EXCLUDED.environment_id,
		    environment = EXCLUDED.environment, blocked_reason = EXCLUDED.blocked_reason`, runID, envID, env, blocked)
	return err
}

// GetRunEnvironment returns a run's target (ok=false: no environment).
func (s *Store) GetRunEnvironment(ctx context.Context, runID uuid.UUID) (RunEnvironment, bool, error) {
	var e RunEnvironment
	err := s.pool.QueryRow(ctx,
		`SELECT environment, blocked_reason FROM pipeline_run_environments WHERE run_id = $1`, runID).Scan(&e.Environment, &e.BlockedReason)
	if errors.Is(err, pgx.ErrNoRows) {
		return RunEnvironment{}, false, nil
	}
	return e, err == nil, err
}

// DeployFacts are the commit's history the environment gate needs.
type DeployFacts struct {
	// SucceededEnvironments are environments this commit deployed to
	// successfully (in this repository).
	SucceededEnvironments []string
	// RunSucceeded: some non-deploy run of the commit is green.
	RunSucceeded bool
	// FirstSeen is when the commit's first run was created (zero: never).
	FirstSeen time.Time
}

// CommitDeployFacts gathers the gate's facts for repo@sha.
func (s *Store) CommitDeployFacts(ctx context.Context, repoID uuid.UUID, sha string) (DeployFacts, error) {
	var f DeployFacts
	err := s.pool.QueryRow(ctx, `
		SELECT
		  COALESCE(array_agg(DISTINCT e.environment) FILTER (WHERE r.status = 'success' AND e.run_id IS NOT NULL AND e.blocked_reason = ''), '{}'),
		  COALESCE(bool_or(r.status = 'success' AND e.run_id IS NULL), false),
		  COALESCE(min(r.created_at), 'epoch'::timestamptz)
		FROM pipeline_runs r
		JOIN pipelines p ON p.id = r.pipeline_id
		LEFT JOIN pipeline_run_environments e ON e.run_id = r.id
		WHERE p.repo_id = $1 AND r.commit_sha = $2`, repoID, sha).Scan(&f.SucceededEnvironments, &f.RunSucceeded, &f.FirstSeen)
	if f.FirstSeen.Unix() == 0 {
		f.FirstSeen = time.Time{}
	}
	return f, err
}

// SupersedePendingRuns cancels the older push runs of a pipeline on ref that
// are still waiting for a worker (queue row unclaimed), keeping keep. A burst of
// pushes/merges to one branch then runs only its newest commit, instead of
// queueing one deploy per merge. Returns the cancelled run numbers.
// SupersedePendingRuns cancels older waiting push runs of the pipeline on
// ref. With includeSameCommit false, only runs of a different (older) commit
// than the kept run go: a re-run of the same commit is deliberate.
func (s *Store) SupersedePendingRuns(ctx context.Context, pipelineID uuid.UUID, ref string, keep uuid.UUID, includeSameCommit ...bool) ([]int64, error) {
	same := len(includeSameCommit) == 0 || includeSameCommit[0]
	rows, err := s.pool.Query(ctx, `
		WITH old AS (
			SELECT r.id FROM pipeline_runs r
			JOIN pipeline_queue q ON q.run_id = r.id AND q.status = 'pending'
			WHERE r.pipeline_id = $1 AND r.ref = $2 AND r.id <> $3 AND r.status = 'pending' AND r.event = 'push'
			  AND r.created_at <= (SELECT created_at FROM pipeline_runs WHERE id = $3)
			  AND ($4 OR r.commit_sha <> (SELECT commit_sha FROM pipeline_runs WHERE id = $3))
		), withdrawn AS (
			UPDATE pipeline_queue SET status = 'done', updated_at = now()
			WHERE run_id IN (SELECT id FROM old) AND status = 'pending'
		)
		UPDATE pipeline_runs SET status = 'cancelled', finished_at = now()
		WHERE id IN (SELECT id FROM old)
		RETURNING run_number`, pipelineID, ref, keep, same)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var n int64
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// WithdrawPendingRunsForRef cancels every waiting push run on ref in the
// repository (all workflows): used when the branch is deleted, or merged so
// that the base branch's own runs cover its commits. Running jobs finish.
func (s *Store) WithdrawPendingRunsForRef(ctx context.Context, repoID uuid.UUID, ref string) ([]int64, error) {
	rows, err := s.pool.Query(ctx, `
		WITH old AS (
			SELECT r.id FROM pipeline_runs r
			JOIN pipelines p ON p.id = r.pipeline_id
			JOIN pipeline_queue q ON q.run_id = r.id AND q.status = 'pending'
			WHERE p.repo_id = $1 AND r.ref = $2 AND r.status = 'pending' AND r.event = 'push'
		), withdrawn AS (
			UPDATE pipeline_queue SET status = 'done', updated_at = now()
			WHERE run_id IN (SELECT id FROM old) AND status = 'pending'
		)
		UPDATE pipeline_runs SET status = 'cancelled', finished_at = now()
		WHERE id IN (SELECT id FROM old)
		RETURNING run_number`, repoID, ref)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var n int64
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}
