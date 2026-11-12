package store

import (
	"context"
	"errors"
	"fmt"
	"time"

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

// ErrRepoSlugAmbiguous means a project-slug+repo-slug pair matches repositories
// in more than one tenant: the unscoped B2 resolver refuses to pick one.
var ErrRepoSlugAmbiguous = errors.New("repo slugs ambiguous across tenants")

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

// GitRepoRef identifies one repository for the git smart-HTTP read path.
type GitRepoRef struct {
	RepoID      uuid.UUID
	TenantID    uuid.UUID
	ProjectID   uuid.UUID
	ProjectSlug string
	RepoSlug    string
}

// ListEngineRepos enumerates every repository with a referee row — the set
// the own git engine owns, whose manifests maintenance (B7) walks.
func (s *Store) ListEngineRepos(ctx context.Context) ([]GitRepoRef, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT r.id, p.tenant_id, p.id, p.slug, r.slug
		FROM git_repo_state g
		JOIN repositories r ON r.id = g.repo_id
		JOIN projects p ON p.id = r.project_id
		ORDER BY r.slug`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []GitRepoRef
	for rows.Next() {
		var ref GitRepoRef
		if err := rows.Scan(&ref.RepoID, &ref.TenantID, &ref.ProjectID, &ref.ProjectSlug, &ref.RepoSlug); err != nil {
			return nil, err
		}
		out = append(out, ref)
	}
	return out, rows.Err()
}

// FindRepoForGit resolves project+repo slugs to the repo's identity for the
// git engine (tenant for the bucket key prefix, slugs for the layout). This is
// the interim B2 resolver: slugs are not globally unique, and a collision
// across tenants is an explicit error rather than a silent wrong-repo serve —
// B6's token-scoped resolution will disambiguate by the authenticated user.
func (s *Store) FindRepoForGit(ctx context.Context, projectSlug, repoSlug string) (GitRepoRef, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT r.id, p.tenant_id, p.id, p.slug, r.slug
		FROM repositories r
		JOIN projects p ON p.id = r.project_id
		WHERE lower(p.slug) = lower($1) AND lower(r.slug) = lower($2)`,
		projectSlug, repoSlug)
	if err != nil {
		return GitRepoRef{}, err
	}
	defer rows.Close()
	var refs []GitRepoRef
	for rows.Next() {
		var ref GitRepoRef
		if err := rows.Scan(&ref.RepoID, &ref.TenantID, &ref.ProjectID, &ref.ProjectSlug, &ref.RepoSlug); err != nil {
			return GitRepoRef{}, err
		}
		refs = append(refs, ref)
	}
	switch len(refs) {
	case 1:
		return refs[0], nil
	case 0:
		return GitRepoRef{}, pgx.ErrNoRows
	default:
		return GitRepoRef{}, ErrRepoSlugAmbiguous
	}
}

// AcquirePushLease takes a repo's write lease for holder: it wins when the
// row is free, expired (crashed leaser), or already held by the same holder
// (retry after a network blip). Returns false when another live holder owns
// the lease. The TTL bounds how long a crashed push can block a repo.
func (s *Store) AcquirePushLease(ctx context.Context, repoID uuid.UUID, holder string, ttl time.Duration) (bool, error) {
	const q = `
		INSERT INTO push_leases (repo_id, holder, expires)
		VALUES ($1, $2, now() + $3::interval)
		ON CONFLICT (repo_id) DO UPDATE
			SET holder = $2, expires = now() + $3::interval
			WHERE push_leases.expires < now() OR push_leases.holder = $2
		RETURNING holder`
	var got string
	err := s.pool.QueryRow(ctx, q, repoID, holder, ttl.String()).Scan(&got)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return got == holder, nil
}

// ReleasePushLease frees the lease, but only when holder still owns it — a
// lease stolen after expiry must not be released by its previous holder.
func (s *Store) ReleasePushLease(ctx context.Context, repoID uuid.UUID, holder string) error {
	const q = `DELETE FROM push_leases WHERE repo_id = $1 AND holder = $2`
	_, err := s.pool.Exec(ctx, q, repoID, holder)
	if err != nil {
		return fmt.Errorf("release push lease: %w", err)
	}
	return nil
}
