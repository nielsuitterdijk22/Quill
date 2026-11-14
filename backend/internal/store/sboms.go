package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// SBOM is one sboms row (Document is loaded only by GetSBOMDocument).
type SBOM struct {
	ID             uuid.UUID
	RepoID         uuid.UUID
	TenantID       uuid.UUID
	CommitSHA      string
	Ref            string
	Status         string
	Format         string
	SpecVersion    string
	ComponentCount int
	Error          string
	CreatedAt      time.Time
	StartedAt      *time.Time
	FinishedAt     *time.Time
}

// SBOM statuses.
const (
	SBOMPending = "pending"
	SBOMRunning = "running"
	SBOMReady   = "ready"
	SBOMFailed  = "failed"
)

// sbomRetention is how many SBOMs per repo are kept.
const sbomRetention = 10

const sbomColumns = `id, repo_id, tenant_id, commit_sha, ref, status, format, spec_version,
	component_count, error, created_at, started_at, finished_at`

func scanSBOM(row pgx.Row) (SBOM, error) {
	var b SBOM
	err := row.Scan(&b.ID, &b.RepoID, &b.TenantID, &b.CommitSHA, &b.Ref, &b.Status, &b.Format,
		&b.SpecVersion, &b.ComponentCount, &b.Error, &b.CreatedAt, &b.StartedAt, &b.FinishedAt)
	return b, err
}

// QueueSBOM records a pending SBOM for (repo, commit). created reports whether
// a new row was made; an existing ready/running row is left alone, a failed
// one — or a pending/running one abandoned for 15+ minutes (an instance died
// mid-generation) — is reset to pending.
func (s *Store) QueueSBOM(ctx context.Context, repoID, tenantID uuid.UUID, commitSHA, ref string) (SBOM, bool, error) {
	row := s.pool.QueryRow(ctx, `
		INSERT INTO sboms (repo_id, tenant_id, commit_sha, ref)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (repo_id, commit_sha) DO UPDATE
			SET status = 'pending', error = '', started_at = NULL, finished_at = NULL,
			    created_at = now(), ref = EXCLUDED.ref
			WHERE sboms.status = 'failed'
			   OR (sboms.status IN ('pending', 'running') AND sboms.created_at < now() - interval '15 minutes')
		RETURNING `+sbomColumns+`, (xmax = 0)`, repoID, tenantID, commitSHA, ref)
	var b SBOM
	var inserted bool
	err := row.Scan(&b.ID, &b.RepoID, &b.TenantID, &b.CommitSHA, &b.Ref, &b.Status, &b.Format,
		&b.SpecVersion, &b.ComponentCount, &b.Error, &b.CreatedAt, &b.StartedAt, &b.FinishedAt, &inserted)
	if errors.Is(err, pgx.ErrNoRows) {
		// Conflict with a pending/running/ready row: nothing to do.
		existing, gerr := s.getSBOM(ctx, "repo_id = $1 AND commit_sha = $2", repoID, commitSHA)
		return existing, false, gerr
	}
	if err != nil {
		return SBOM{}, false, err
	}
	if inserted {
		_, _ = s.pool.Exec(ctx, `
			DELETE FROM sboms WHERE repo_id = $1 AND id NOT IN (
				SELECT id FROM sboms WHERE repo_id = $1 ORDER BY created_at DESC LIMIT $2)`,
			repoID, sbomRetention)
	}
	return b, true, nil
}

func (s *Store) getSBOM(ctx context.Context, where string, args ...any) (SBOM, error) {
	return scanSBOM(s.pool.QueryRow(ctx, "SELECT "+sbomColumns+" FROM sboms WHERE "+where, args...))
}

// LatestSBOM returns the newest SBOM row for a repo (any status).
func (s *Store) LatestSBOM(ctx context.Context, repoID uuid.UUID) (SBOM, error) {
	return s.getSBOM(ctx, "repo_id = $1 ORDER BY created_at DESC LIMIT 1", repoID)
}

// LatestReadySBOM returns the newest ready SBOM for a repo.
func (s *Store) LatestReadySBOM(ctx context.Context, repoID uuid.UUID) (SBOM, error) {
	return s.getSBOM(ctx, "repo_id = $1 AND status = 'ready' ORDER BY created_at DESC LIMIT 1", repoID)
}

// SBOMByCommit returns the SBOM for one commit.
func (s *Store) SBOMByCommit(ctx context.Context, repoID uuid.UUID, commitSHA string) (SBOM, error) {
	return s.getSBOM(ctx, "repo_id = $1 AND commit_sha = $2", repoID, commitSHA)
}

// StartSBOM claims a pending row (pending → running). False: someone else did.
func (s *Store) StartSBOM(ctx context.Context, id uuid.UUID) (bool, error) {
	tag, err := s.pool.Exec(ctx,
		"UPDATE sboms SET status = 'running', started_at = now() WHERE id = $1 AND status = 'pending'", id)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

// FinishSBOM stores a generated document.
func (s *Store) FinishSBOM(ctx context.Context, id uuid.UUID, specVersion string, components int, doc []byte) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE sboms SET status = 'ready', spec_version = $2, component_count = $3,
			document = $4, error = '', finished_at = now()
		WHERE id = $1`, id, specVersion, components, doc)
	return err
}

// FailSBOM records a generation failure.
func (s *Store) FailSBOM(ctx context.Context, id uuid.UUID, msg string) error {
	if len(msg) > 2000 {
		msg = msg[:2000]
	}
	_, err := s.pool.Exec(ctx, `
		UPDATE sboms SET status = 'failed', error = $2, finished_at = now() WHERE id = $1`, id, msg)
	return err
}

// GetSBOMDocument loads the stored document bytes.
func (s *Store) GetSBOMDocument(ctx context.Context, id uuid.UUID) ([]byte, error) {
	var doc []byte
	err := s.pool.QueryRow(ctx, "SELECT document FROM sboms WHERE id = $1", id).Scan(&doc)
	return doc, err
}
