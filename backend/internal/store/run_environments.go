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
