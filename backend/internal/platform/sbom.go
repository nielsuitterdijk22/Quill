package platform

import (
	"archive/tar"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/nielsuitterdijk22/quill/internal/store"
)

// SBOM per default-branch push (2026-11-13). Not a pipeline: the api archives
// the pushed commit out of the hydrated bare repo and runs an SBOMGenerator
// (syft in production) over the tree; the CycloneDX document lands in
// Postgres and the repo landing page shows its status + a download button.

// SBOMGenerator turns a source tree into a CycloneDX JSON document.
type SBOMGenerator interface {
	Generate(ctx context.Context, dir string) ([]byte, error)
}

// SyftGenerator runs the syft binary baked into the backend image.
type SyftGenerator struct {
	Bin string // default "syft"
}

// Generate implements SBOMGenerator.
func (g SyftGenerator) Generate(ctx context.Context, dir string) ([]byte, error) {
	bin := g.Bin
	if bin == "" {
		bin = "syft"
	}
	cmd := exec.CommandContext(ctx, bin, "scan", "dir:"+dir, "-o", "cyclonedx-json", "--quiet")
	// syft caches DBs/state under HOME/XDG dirs: keep them in the scratch area.
	cmd.Env = append(os.Environ(), "SYFT_CHECK_FOR_APP_UPDATE=false", "XDG_CACHE_HOME="+filepath.Join(os.TempDir(), "syft-cache"))
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if len(msg) > 500 {
			msg = msg[len(msg)-500:]
		}
		return nil, fmt.Errorf("syft: %w: %s", err, msg)
	}
	return out, nil
}

// WithSBOMGenerator enables SBOM generation (nil disables it).
func (s *Service) WithSBOMGenerator(g SBOMGenerator) *Service {
	s.sbomGen = g
	return s
}

const (
	// sbomTimeout bounds one generation (archive + scan).
	sbomTimeout = 5 * time.Minute
	// sbomStaleAfter: a pending/running row older than this was lost (the
	// instance scaled down mid-generation) and is reported as failed.
	sbomStaleAfter = 15 * time.Minute
	// sbomMaxTreeBytes caps the extracted tree (RAM-backed /tmp on serverless).
	sbomMaxTreeBytes = 512 << 20
)

// SBOMView is the landing page's SBOM status.
type SBOMView struct {
	Status         string     `json:"status"` // pending | running | ready | failed
	CommitSHA      string     `json:"commitSha"`
	Ref            string     `json:"ref"`
	Format         string     `json:"format"`
	SpecVersion    string     `json:"specVersion,omitempty"`
	ComponentCount int        `json:"componentCount"`
	Error          string     `json:"error,omitempty"`
	CreatedAt      time.Time  `json:"createdAt"`
	FinishedAt     *time.Time `json:"finishedAt,omitempty"`
}

func sbomView(b store.SBOM) *SBOMView {
	v := &SBOMView{
		Status: b.Status, CommitSHA: b.CommitSHA, Ref: b.Ref, Format: b.Format,
		SpecVersion: b.SpecVersion, ComponentCount: b.ComponentCount, Error: b.Error,
		CreatedAt: b.CreatedAt, FinishedAt: b.FinishedAt,
	}
	if (b.Status == store.SBOMPending || b.Status == store.SBOMRunning) && time.Since(b.CreatedAt) > sbomStaleAfter {
		v.Status = store.SBOMFailed
		v.Error = "generation did not finish (interrupted); regenerate to retry"
	}
	return v
}

// queueSBOM records + starts generation for (repo, commit). Idempotent per
// commit: an existing pending/running/ready row is left alone.
func (s *Service) queueSBOM(ctx context.Context, be gitBackend, commitSHA, ref string) (store.SBOM, error) {
	if s.sbomGen == nil {
		return store.SBOM{}, fmt.Errorf("%w: SBOM generation is not configured", ErrUnavailable)
	}
	row, created, err := s.store.QueueSBOM(ctx, be.repoID, be.tenantID, commitSHA, ref)
	if err != nil {
		return store.SBOM{}, err
	}
	if created {
		go s.generateSBOM(row.ID, be, commitSHA)
	}
	return row, nil
}

// generateSBOM runs detached from the triggering request; one generation at a
// time per instance (the semaphore protects a 1 GiB container's memory).
func (s *Service) generateSBOM(id uuid.UUID, be gitBackend, commitSHA string) {
	s.sbomSem <- struct{}{}
	defer func() { <-s.sbomSem }()
	ctx, cancel := context.WithTimeout(context.Background(), sbomTimeout)
	defer cancel()
	if ok, err := s.store.StartSBOM(ctx, id); err != nil || !ok {
		return
	}
	fail := func(err error) {
		s.logger.Warn("sbom generation failed", "repo", be.projectSlug+"/"+be.repoSlug, "commit", commitSHA, "error", err)
		_ = s.store.FailSBOM(context.Background(), id, err.Error())
	}
	dir, err := s.engineDir(ctx, be)
	if err != nil {
		fail(fmt.Errorf("hydrate: %w", err))
		return
	}
	tree, err := os.MkdirTemp("", "quill-sbom-")
	if err != nil {
		fail(err)
		return
	}
	defer os.RemoveAll(tree)
	if err := archiveCommit(ctx, dir, commitSHA, tree); err != nil {
		fail(fmt.Errorf("archive %s: %w", commitSHA, err))
		return
	}
	doc, err := s.sbomGen.Generate(ctx, tree)
	if err != nil {
		fail(err)
		return
	}
	var parsed struct {
		BOMFormat   string            `json:"bomFormat"`
		SpecVersion string            `json:"specVersion"`
		Components  []json.RawMessage `json:"components"`
	}
	if err := json.Unmarshal(doc, &parsed); err != nil || parsed.BOMFormat != "CycloneDX" {
		fail(fmt.Errorf("generator output is not a CycloneDX JSON document"))
		return
	}
	if err := s.store.FinishSBOM(context.Background(), id, parsed.SpecVersion, len(parsed.Components), doc); err != nil {
		fail(err)
	}
}

// archiveCommit extracts `git archive <sha>` into dest without a tar binary.
// Only regular files and directories are materialized (symlinks are skipped:
// a scanner never needs them and they could point outside dest).
func archiveCommit(ctx context.Context, repoDir, sha, dest string) error {
	cmd := exec.CommandContext(ctx, "git", "archive", "--format=tar", sha)
	cmd.Dir = repoDir
	out, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return err
	}
	extractErr := func() error {
		tr := tar.NewReader(out)
		var total int64
		root := filepath.Clean(dest) + string(os.PathSeparator)
		for {
			h, err := tr.Next()
			if errors.Is(err, io.EOF) {
				return nil
			}
			if err != nil {
				return err
			}
			target := filepath.Join(dest, filepath.Clean("/"+h.Name))
			if !strings.HasPrefix(target+string(os.PathSeparator), root) {
				continue
			}
			switch h.Typeflag {
			case tar.TypeDir:
				if err := os.MkdirAll(target, 0o755); err != nil {
					return err
				}
			case tar.TypeReg:
				total += h.Size
				if total > sbomMaxTreeBytes {
					return fmt.Errorf("tree larger than %d MiB", sbomMaxTreeBytes>>20)
				}
				if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
					return err
				}
				f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
				if err != nil {
					return err
				}
				_, cerr := io.Copy(f, tr)
				f.Close()
				if cerr != nil {
					return cerr
				}
			}
		}
	}()
	if extractErr != nil {
		_, _ = io.Copy(io.Discard, out)
	}
	waitErr := cmd.Wait()
	if extractErr != nil {
		return extractErr
	}
	if waitErr != nil {
		return fmt.Errorf("%w: %s", waitErr, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// sbomRepo resolves an engine repo for SBOM reads (members + public repos).
func (s *Service) sbomRepo(ctx context.Context, actor Actor, projectSlug, repoSlug string) (gitBackend, error) {
	_, be, err := s.resolveBrowseRepo(ctx, actor, projectSlug, repoSlug, true)
	if err != nil {
		return gitBackend{}, err
	}
	if !be.engine {
		return gitBackend{}, fmt.Errorf("%w: SBOMs are available for engine-backed repositories", ErrUnavailable)
	}
	return be, nil
}

// RepoSBOM returns the latest SBOM status (nil when none exists yet).
func (s *Service) RepoSBOM(ctx context.Context, actor Actor, projectSlug, repoSlug string) (*SBOMView, error) {
	be, err := s.sbomRepo(ctx, actor, projectSlug, repoSlug)
	if err != nil {
		return nil, err
	}
	row, err := s.store.LatestSBOM(ctx, be.repoID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return sbomView(row), nil
}

// RequestRepoSBOM (re)generates the SBOM for the default branch head — the
// button for repos without one yet and the retry after a failure. Members only.
func (s *Service) RequestRepoSBOM(ctx context.Context, actor Actor, projectSlug, repoSlug string) (*SBOMView, error) {
	repo, be, err := s.resolvePullRepo(ctx, actor, projectSlug, repoSlug)
	if err != nil {
		return nil, err
	}
	if !be.engine {
		return nil, fmt.Errorf("%w: SBOMs are available for engine-backed repositories", ErrUnavailable)
	}
	src := pipelineSource{repo: repo, be: be}
	ref := "refs/heads/" + repo.DefaultBranch
	sha := s.commitSHA(ctx, src, ref)
	if sha == "" {
		return nil, fmt.Errorf("%w: the default branch has no commits yet", ErrInvalidInput)
	}
	row, err := s.queueSBOM(ctx, be, sha, ref)
	if err != nil {
		return nil, err
	}
	return sbomView(row), nil
}

// RepoSBOMDocument returns a ready SBOM document (latest when sha is empty)
// plus a download file name.
func (s *Service) RepoSBOMDocument(ctx context.Context, actor Actor, projectSlug, repoSlug, sha string) ([]byte, string, error) {
	be, err := s.sbomRepo(ctx, actor, projectSlug, repoSlug)
	if err != nil {
		return nil, "", err
	}
	var row store.SBOM
	if sha == "" {
		row, err = s.store.LatestReadySBOM(ctx, be.repoID)
	} else {
		row, err = s.store.SBOMByCommit(ctx, be.repoID, sha)
	}
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && row.Status != store.SBOMReady) {
		return nil, "", ErrNotFound
	}
	if err != nil {
		return nil, "", err
	}
	doc, err := s.store.GetSBOMDocument(ctx, row.ID)
	if err != nil {
		return nil, "", err
	}
	short := row.CommitSHA
	if len(short) > 7 {
		short = short[:7]
	}
	return doc, fmt.Sprintf("%s-%s.cdx.json", be.repoSlug, short), nil
}
