package store

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ErrRefCASLost means a ref-update compare-and-swap raced: another writer
// committed a newer revision of the repository's state first. The caller must
// re-read the state, re-apply its ref transition on the fresh revision and
// retry (bounded), or fail with the "fetch and retry" message git clients
// understand. This is the referee's only lost-race signal — never a bug when
// it surfaces precisely because two writers believed the same revision.
var ErrRefCASLost = errors.New("ref-cas lost race")

// GitRepoState is the referee row for one repository.
type GitRepoState struct {
	RepoID       uuid.UUID
	TenantID     *uuid.UUID
	Revision     int64
	Refs         map[string]string
	StorageBytes int64
}

const gitStateColumns = "repo_id, tenant_id, revision, refs, storage_bytes"

// EnsureGitRepoState idempotently creates the referee row for a repo (the
// state itself is never reset: provisioning twice keeps the existing refs
// and revision — only the tenant stamp is written when the row is new).
func (s *Store) EnsureGitRepoState(ctx context.Context, repoID, tenantID uuid.UUID) (GitRepoState, error) {
	// Typically called inside the provisioning transaction (q.Exec path in
	// platform); this Store-level method resolves after the fact.
	const q = `
		INSERT INTO git_repo_state (repo_id, tenant_id)
		VALUES ($1, $2)
		ON CONFLICT (repo_id) DO NOTHING`
	if _, err := s.pool.Exec(ctx, q, repoID, tenantID); err != nil {
		return GitRepoState{}, err
	}
	return s.GetGitRepoState(ctx, repoID)
}

// GetGitRepoState loads the referee row.
func (s *Store) GetGitRepoState(ctx context.Context, repoID uuid.UUID) (GitRepoState, error) {
	const q = "SELECT " + gitStateColumns + " FROM git_repo_state WHERE repo_id = $1"
	var st GitRepoState
	var tenantID *uuid.UUID
	err := s.pool.QueryRow(ctx, q, repoID).Scan(
		&st.RepoID, &tenantID, &st.Revision, &st.Refs, &st.StorageBytes)
	if err != nil {
		return GitRepoState{}, err
	}
	st.TenantID = tenantID
	return st, nil
}

// CASGitRepoRefs compare-and-swaps the refs map: the update only lands when
// the row is still at fromRevision; on success the revision advances by one.
// A lost race returns ErrRefCASLost; an unknown repo returns pgx.ErrNoRows.
func (s *Store) CASGitRepoRefs(ctx context.Context, repoID uuid.UUID, fromRevision int64, refs map[string]string, storageBytes int64) (int64, error) {
	const q = `
		UPDATE git_repo_state
		SET refs = $3::jsonb, storage_bytes = $4, revision = revision + 1, updated_at = now()
		WHERE repo_id = $1 AND revision = $2
		RETURNING revision`
	var newRev int64
	err := s.pool.QueryRow(ctx, q, repoID, fromRevision, refs, storageBytes).Scan(&newRev)
	if errors.Is(err, pgx.ErrNoRows) {
		// Distinguish lost race from missing row: a repo without a referee row
		// is a provisioning bug, not a race.
		var exists bool
		if err := s.pool.QueryRow(ctx,
			"SELECT EXISTS (SELECT 1 FROM git_repo_state WHERE repo_id = $1)", repoID).Scan(&exists); err != nil {
			return 0, err
		}
		if exists {
			return 0, ErrRefCASLost
		}
		return 0, pgx.ErrNoRows
	}
	if err != nil {
		return 0, err
	}
	return newRev, nil
}
