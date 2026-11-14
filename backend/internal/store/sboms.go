package store

import (
	"context"
	"errors"
	"strings"
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
const sbomRetention = 30

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
		// Conflict with a pending/running/ready row: nothing to do, except
		// promoting a row generated on request (empty ref) to the branch
		// head it has since become.
		if ref != "" {
			_, _ = s.pool.Exec(ctx, "UPDATE sboms SET ref = $3 WHERE repo_id = $1 AND commit_sha = $2 AND ref = ''", repoID, commitSHA, ref)
		}
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

// LatestSBOM returns the newest default-branch SBOM row for a repo (any
// status). Rows with an empty ref were generated on request for an older
// commit and never stand in for the branch head.
func (s *Store) LatestSBOM(ctx context.Context, repoID uuid.UUID) (SBOM, error) {
	return s.getSBOM(ctx, "repo_id = $1 AND ref <> '' ORDER BY created_at DESC LIMIT 1", repoID)
}

// LatestReadySBOM returns the newest ready SBOM for a repo.
func (s *Store) LatestReadySBOM(ctx context.Context, repoID uuid.UUID) (SBOM, error) {
	return s.getSBOM(ctx, "repo_id = $1 AND status = 'ready' AND ref <> '' ORDER BY created_at DESC LIMIT 1", repoID)
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

// sbomComponentsInsert extracts one SBOM's components into sbom_components
// (the same extraction migration 000033 backfilled with).
const sbomComponentsInsert = `
INSERT INTO sbom_components (sbom_id, repo_id, tenant_id, name, version, ecosystem, license, purl, location)
SELECT s.id, s.repo_id, s.tenant_id,
    CASE WHEN COALESCE(c->>'group', '') <> '' THEN (c->>'group') || '/' || COALESCE(c->>'name', '')
         ELSE COALESCE(c->>'name', '') END,
    COALESCE(c->>'version', ''),
    COALESCE(substring(c->>'purl' from '^pkg:([^/]+)/'), c->>'type', 'other'),
    COALESCE((SELECT string_agg(COALESCE(l->>'expression', l->'license'->>'id', l->'license'->>'name'), ' OR ')
              FROM jsonb_array_elements(CASE WHEN jsonb_typeof(c->'licenses') = 'array' THEN c->'licenses' ELSE '[]'::jsonb END) l), ''),
    COALESCE(c->>'purl', ''),
    COALESCE((SELECT p->>'value'
              FROM jsonb_array_elements(CASE WHEN jsonb_typeof(c->'properties') = 'array' THEN c->'properties' ELSE '[]'::jsonb END) p
              WHERE p->>'name' = 'syft:location:0:path' LIMIT 1), '')
FROM sboms s
CROSS JOIN LATERAL (SELECT convert_from(s.document, 'UTF8')::jsonb AS d) doc
CROSS JOIN LATERAL jsonb_array_elements(
    CASE WHEN jsonb_typeof(doc.d->'components') = 'array' THEN doc.d->'components' ELSE '[]'::jsonb END) c
WHERE s.id = $1 AND COALESCE(c->>'name', '') <> ''`

// FinishSBOM stores a generated document and indexes its components for
// cross-repo search, atomically.
func (s *Store) FinishSBOM(ctx context.Context, id uuid.UUID, specVersion string, components int, doc []byte) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `
		UPDATE sboms SET status = 'ready', spec_version = $2, component_count = $3,
			document = $4, error = '', finished_at = now()
		WHERE id = $1`, id, specVersion, components, doc); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, "DELETE FROM sbom_components WHERE sbom_id = $1", id); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, sbomComponentsInsert, id); err != nil {
		return err
	}
	return tx.Commit(ctx)
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

// SBOMHistory lists a repo's SBOM rows, newest first (documents not loaded).
func (s *Store) SBOMHistory(ctx context.Context, repoID uuid.UUID, limit int) ([]SBOM, error) {
	rows, err := s.pool.Query(ctx, "SELECT "+sbomColumns+" FROM sboms WHERE repo_id = $1 ORDER BY created_at DESC LIMIT $2", repoID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SBOM
	for rows.Next() {
		b, err := scanSBOM(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// SBOMComponentHit is one component of a repo's current SBOM matching a
// cross-repo search.
type SBOMComponentHit struct {
	ProjectSlug string
	RepoSlug    string
	CommitSHA   string
	Name        string
	Version     string
	Ecosystem   string
	License     string
	Purl        string
}

// latestHeadSBOMs selects, per repo of the given projects, the newest ready
// default-branch SBOM.
const latestHeadSBOMs = `
	SELECT DISTINCT ON (s.repo_id) s.id, s.repo_id, s.commit_sha
	FROM sboms s JOIN repositories r ON r.id = s.repo_id
	WHERE r.project_id = ANY($1) AND s.status = 'ready' AND s.ref <> ''
	ORDER BY s.repo_id, s.created_at DESC`

// SearchSBOMComponents matches components (name, purl or license contains
// query, case-insensitive; every component when query is empty) in the
// current SBOM of each repo in projectIDs. It also reports how many repos
// have an SBOM to search.
func (s *Store) SearchSBOMComponents(ctx context.Context, projectIDs []uuid.UUID, query string, limit int) ([]SBOMComponentHit, int, error) {
	if len(projectIDs) == 0 {
		return nil, 0, nil
	}
	var repos int
	if err := s.pool.QueryRow(ctx, "SELECT count(*) FROM ("+latestHeadSBOMs+") l", projectIDs).Scan(&repos); err != nil {
		return nil, 0, err
	}
	pattern := "%" + likeEscaper.Replace(strings.ToLower(query)) + "%"
	rows, err := s.pool.Query(ctx, `
		WITH latest AS (`+latestHeadSBOMs+`)
		SELECT DISTINCT p.slug, r.slug, l.commit_sha, c.name, c.version, c.ecosystem, c.license, c.purl
		FROM latest l
		JOIN sbom_components c ON c.sbom_id = l.id
		JOIN repositories r ON r.id = l.repo_id
		JOIN projects p ON p.id = r.project_id
		WHERE $2 = '%%' OR lower(c.name) LIKE $2 OR lower(c.purl) LIKE $2 OR lower(c.license) LIKE $2
		ORDER BY c.name, c.version, p.slug, r.slug
		LIMIT $3`, projectIDs, pattern, limit)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []SBOMComponentHit
	for rows.Next() {
		var h SBOMComponentHit
		if err := rows.Scan(&h.ProjectSlug, &h.RepoSlug, &h.CommitSHA, &h.Name, &h.Version, &h.Ecosystem, &h.License, &h.Purl); err != nil {
			return nil, 0, err
		}
		out = append(out, h)
	}
	return out, repos, rows.Err()
}

var likeEscaper = strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`)
