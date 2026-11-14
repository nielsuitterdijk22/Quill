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
	"sort"
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

// SBOMStatus is what the repo landing page shows: the SBOM of the default
// branch head (Current, possibly still generating) and, when that is not
// ready yet, the newest ready one so there is always a document to open.
type SBOMStatus struct {
	Current     *SBOMView `json:"sbom"`
	LatestReady *SBOMView `json:"latestReady,omitempty"`
}

// defaultHead returns the default branch's head commit from the referee row
// (no hydration) — "" for an empty repo.
func (s *Service) defaultHead(ctx context.Context, repoID uuid.UUID, defaultBranch string) (string, string, error) {
	st, err := s.store.GetGitRepoState(ctx, repoID)
	if err != nil {
		return "", "", err
	}
	ref := "refs/heads/" + defaultBranch
	return st.Refs[ref], ref, nil
}

// EnsureRepoSBOM is the landing page read (2026-11-14: SBOMs are always
// there, never on request). It makes sure the default branch head has an
// SBOM — queueing generation when a head somehow has none yet (repos older
// than the feature, a missed push hook, an interrupted generation) — and
// returns the head's status plus the newest ready document.
func (s *Service) EnsureRepoSBOM(ctx context.Context, actor Actor, projectSlug, repoSlug string) (SBOMStatus, error) {
	repo, be, err := s.resolveBrowseRepo(ctx, actor, projectSlug, repoSlug, true)
	if err != nil {
		return SBOMStatus{}, err
	}
	if !be.engine {
		return SBOMStatus{}, fmt.Errorf("%w: SBOMs are available for engine-backed repositories", ErrUnavailable)
	}
	var out SBOMStatus
	head, ref, err := s.defaultHead(ctx, be.repoID, repo.DefaultBranch)
	if err != nil {
		return SBOMStatus{}, err
	}
	var current store.SBOM
	switch {
	case head != "" && s.sbomGen != nil:
		current, err = s.queueSBOM(ctx, be, head, ref)
	default:
		current, err = s.store.LatestSBOM(ctx, be.repoID)
		if errors.Is(err, pgx.ErrNoRows) {
			return out, nil
		}
	}
	if err != nil {
		return SBOMStatus{}, err
	}
	out.Current = sbomView(current)
	if current.Status != store.SBOMReady {
		if ready, err := s.store.LatestReadySBOM(ctx, be.repoID); err == nil && ready.ID != current.ID {
			out.LatestReady = sbomView(ready)
		}
	}
	return out, nil
}

// BackfillSBOMs generates, synchronously, the default-branch-head SBOM of
// every engine repo that lacks one (or whose generation failed / was
// interrupted). Run by the daily maintenance job so every repo has its SBOM
// before anyone looks. Returns how many were generated and how many failed.
func (s *Service) BackfillSBOMs(ctx context.Context) (generated, failed int, err error) {
	if s.sbomGen == nil {
		return 0, 0, fmt.Errorf("%w: SBOM generation is not configured", ErrUnavailable)
	}
	repos, err := s.store.ListEngineRepos(ctx)
	if err != nil {
		return 0, 0, err
	}
	for _, r := range repos {
		repo, err := s.store.GetRepositoryByID(ctx, r.RepoID)
		if err != nil {
			failed++
			continue
		}
		head, ref, err := s.defaultHead(ctx, r.RepoID, repo.DefaultBranch)
		if err != nil || head == "" {
			continue
		}
		row, created, err := s.store.QueueSBOM(ctx, r.RepoID, r.TenantID, head, ref)
		if err != nil {
			failed++
			continue
		}
		if !created {
			continue
		}
		be := gitBackend{engine: true, repoID: r.RepoID, tenantID: r.TenantID, projectSlug: r.ProjectSlug, repoSlug: r.RepoSlug}
		s.generateSBOM(row.ID, be, head)
		if done, err := s.store.SBOMByCommit(ctx, r.RepoID, head); err == nil && done.Status == store.SBOMReady {
			generated++
		} else {
			failed++
		}
	}
	return generated, failed, nil
}

// CommitSBOM returns the SBOM of one commit (nil when it has none).
func (s *Service) CommitSBOM(ctx context.Context, actor Actor, projectSlug, repoSlug, sha string) (*SBOMView, error) {
	be, err := s.sbomRepo(ctx, actor, projectSlug, repoSlug)
	if err != nil {
		return nil, err
	}
	row, err := s.store.SBOMByCommit(ctx, be.repoID, sha)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return sbomView(row), nil
}

// RequestCommitSBOM generates the SBOM of an older commit on request
// (members only) — the default branch head always has one already. The row
// keeps an empty ref so it never stands in for the branch head.
func (s *Service) RequestCommitSBOM(ctx context.Context, actor Actor, projectSlug, repoSlug, sha string) (*SBOMView, error) {
	repo, be, err := s.resolvePullRepo(ctx, actor, projectSlug, repoSlug)
	if err != nil {
		return nil, err
	}
	if !be.engine {
		return nil, fmt.Errorf("%w: SBOMs are available for engine-backed repositories", ErrUnavailable)
	}
	src := pipelineSource{repo: repo, be: be}
	if s.commitSHA(ctx, src, sha) != sha {
		return nil, fmt.Errorf("%w: commit %s does not exist in this repository", ErrNotFound, sha)
	}
	ref := ""
	if head, headRef, err := s.defaultHead(ctx, be.repoID, repo.DefaultBranch); err == nil && head == sha {
		ref = headRef
	}
	row, err := s.queueSBOM(ctx, be, sha, ref)
	if err != nil {
		return nil, err
	}
	return sbomView(row), nil
}

// RepoSBOMHistory lists the repo's SBOMs, newest first.
func (s *Service) RepoSBOMHistory(ctx context.Context, actor Actor, projectSlug, repoSlug string) ([]SBOMView, error) {
	be, err := s.sbomRepo(ctx, actor, projectSlug, repoSlug)
	if err != nil {
		return nil, err
	}
	rows, err := s.store.SBOMHistory(ctx, be.repoID, 100)
	if err != nil {
		return nil, err
	}
	out := make([]SBOMView, 0, len(rows))
	for _, r := range rows {
		out = append(out, *sbomView(r))
	}
	return out, nil
}

// SBOM search across repositories (2026-11-14).

const (
	sbomSearchRowLimit = 5000
	sbomSearchPackages = 200
)

// SBOMRepoRef is a repository whose current SBOM contains a package version.
type SBOMRepoRef struct {
	Project   string `json:"project"`
	Repo      string `json:"repo"`
	CommitSHA string `json:"commitSha"`
}

// SBOMPackageVersion is one version of a package and where it is used.
type SBOMPackageVersion struct {
	Version string        `json:"version"`
	License string        `json:"license"`
	Purl    string        `json:"purl"`
	Repos   []SBOMRepoRef `json:"repos"`
}

// SBOMPackage groups a package's versions across repositories.
type SBOMPackage struct {
	Name      string               `json:"name"`
	Ecosystem string               `json:"ecosystem"`
	RepoCount int                  `json:"repoCount"`
	Versions  []SBOMPackageVersion `json:"versions"`
}

// SBOMSearchResult answers a cross-repo component search.
type SBOMSearchResult struct {
	Query         string        `json:"query"`
	ReposSearched int           `json:"reposSearched"`
	Truncated     bool          `json:"truncated"`
	Packages      []SBOMPackage `json:"packages"`
}

// SearchSBOMs searches the current default-branch SBOM of every repository in
// the projects the actor belongs to (optionally one project). An empty query
// lists the most widely used packages.
func (s *Service) SearchSBOMs(ctx context.Context, actor Actor, query, projectSlug string) (SBOMSearchResult, error) {
	query = strings.TrimSpace(query)
	if len(query) > 200 {
		return SBOMSearchResult{}, fmt.Errorf("%w: query too long", ErrInvalidInput)
	}
	out := SBOMSearchResult{Query: query, Packages: []SBOMPackage{}}
	projects, err := s.visibleProjects(ctx, actor, projectSlug)
	if err != nil {
		return out, err
	}
	ids := make([]uuid.UUID, 0, len(projects))
	for _, p := range projects {
		ids = append(ids, p.ID)
	}
	hits, repos, err := s.store.SearchSBOMComponents(ctx, ids, query, sbomSearchRowLimit)
	if err != nil {
		return out, err
	}
	out.ReposSearched = repos
	out.Truncated = len(hits) >= sbomSearchRowLimit
	type pkgKey struct{ name, eco string }
	byPkg := map[pkgKey]*SBOMPackage{}
	var order []pkgKey
	repoSets := map[pkgKey]map[string]bool{}
	for _, h := range hits {
		k := pkgKey{h.Name, h.Ecosystem}
		p, ok := byPkg[k]
		if !ok {
			p = &SBOMPackage{Name: h.Name, Ecosystem: h.Ecosystem}
			byPkg[k] = p
			repoSets[k] = map[string]bool{}
			order = append(order, k)
		}
		ref := SBOMRepoRef{Project: h.ProjectSlug, Repo: h.RepoSlug, CommitSHA: h.CommitSHA}
		repoSets[k][h.ProjectSlug+"/"+h.RepoSlug] = true
		found := false
		for i := range p.Versions {
			if p.Versions[i].Version == h.Version {
				v := &p.Versions[i]
				dup := false
				for _, r := range v.Repos {
					dup = dup || r == ref
				}
				if !dup {
					v.Repos = append(v.Repos, ref)
				}
				if v.License == "" {
					v.License = h.License
				}
				found = true
				break
			}
		}
		if !found {
			p.Versions = append(p.Versions, SBOMPackageVersion{Version: h.Version, License: h.License, Purl: h.Purl, Repos: []SBOMRepoRef{ref}})
		}
	}
	for _, k := range order {
		p := byPkg[k]
		p.RepoCount = len(repoSets[k])
		out.Packages = append(out.Packages, *p)
	}
	sort.SliceStable(out.Packages, func(i, j int) bool {
		a, b := out.Packages[i], out.Packages[j]
		if a.RepoCount != b.RepoCount {
			return a.RepoCount > b.RepoCount
		}
		return strings.ToLower(a.Name) < strings.ToLower(b.Name)
	})
	if len(out.Packages) > sbomSearchPackages {
		out.Packages = out.Packages[:sbomSearchPackages]
		out.Truncated = true
	}
	return out, nil
}
