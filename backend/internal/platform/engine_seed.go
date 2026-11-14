package platform

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/nielsuitterdijk22/quill/internal/gitengine"
	"github.com/nielsuitterdijk22/quill/internal/objectstore"
	"github.com/nielsuitterdijk22/quill/internal/store/db"
)

// Engine-native repository seeding (2026-11-14, Forgejo removal): importing
// an external repository (GitHub import, "clone from URL") and forking clone
// the source as a bare repo (heads + tags; no refs/pull/*), consolidate it
// into one pack and commit it through the same SyncRepoToBucket path a push
// uses (packs → referee CAS → manifest).

const seedCloneTimeout = 10 * time.Minute

// seedSource is a bare-cloneable source: a remote https URL (optionally with
// a bearer-style token) or a local directory (an engine repo's hydrated copy).
type seedSource struct {
	url      string
	token    string
	localDir string
}

// validateImportURL accepts only https URLs whose host does not resolve to a
// loopback, private, link-local or otherwise internal address — an import
// must never become a way to read the platform's own network (instance
// metadata, the database, other containers).
func validateImportURL(raw string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
		return fmt.Errorf("%w: import URL must be https://host/path without credentials", ErrInvalidInput)
	}
	host := u.Hostname()
	ips, err := net.LookupIP(host)
	if err != nil || len(ips) == 0 {
		return fmt.Errorf("%w: cannot resolve %s", ErrInvalidInput, host)
	}
	for _, ip := range ips {
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
			ip.IsUnspecified() || ip.IsMulticast() || ip.IsInterfaceLocalMulticast() {
			return fmt.Errorf("%w: %s resolves to a non-public address", ErrInvalidInput, host)
		}
	}
	return nil
}

// bareClone clones src into a fresh temp dir and returns it plus the
// source's default branch ("" when unknown). The caller removes the dir.
func bareClone(ctx context.Context, src seedSource) (string, string, error) {
	tmp, err := os.MkdirTemp("", "quill-seed-")
	if err != nil {
		return "", "", err
	}
	dir := filepath.Join(tmp, "repo.git")
	ctx, cancel := context.WithTimeout(ctx, seedCloneTimeout)
	defer cancel()
	from := src.url
	args := []string{
		"-c", "protocol.allow=never", "-c", "protocol.https.allow=always",
		"-c", "http.followRedirects=false",
		"clone", "--bare", "--quiet",
	}
	if src.localDir != "" {
		from = src.localDir
		args = append([]string{"-c", "protocol.file.allow=always"}, args...)
		args = append(args, "--no-local")
	} else if strings.HasPrefix(from, "http://") {
		// Only reachable when the URL policy is overridden (tests);
		// validateImportURL admits https only.
		args = append([]string{"-c", "protocol.http.allow=always"}, args...)
	}
	args = append(args, from, dir)
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_PROTOCOL_FROM_USER=0")
	if src.token != "" {
		// Via env-config, never argv (process listings) or the URL (logs).
		basic := base64.StdEncoding.EncodeToString([]byte("x-access-token:" + src.token))
		cmd.Env = append(cmd.Env,
			"GIT_CONFIG_COUNT=1",
			"GIT_CONFIG_KEY_0=http.extraHeader",
			"GIT_CONFIG_VALUE_0=Authorization: Basic "+basic,
		)
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		os.RemoveAll(tmp)
		msg := strings.TrimSpace(string(out))
		if src.token != "" {
			msg = strings.ReplaceAll(msg, src.token, "***")
		}
		return "", "", fmt.Errorf("%w: could not clone the source repository: %s", ErrInvalidInput, msg)
	}
	branch := ""
	if out, err := exec.Command("git", "-C", dir, "symbolic-ref", "--short", "HEAD").Output(); err == nil {
		branch = strings.TrimSpace(string(out))
	}
	return dir, branch, nil
}

// seedEngineRepo commits the bare clone at dir as the content of an engine
// repo whose referee row exists with no refs (freshly provisioned).
func (s *Service) seedEngineRepo(ctx context.Context, repoID, tenantID uuid.UUID, projectSlug, repoSlug, dir, actor string) error {
	refs, err := gitengine.RefSnapshot(dir)
	if err != nil {
		return err
	}
	if len(refs) == 0 {
		return nil // an empty source stays an empty repo
	}
	if out, err := exec.CommandContext(ctx, "git", "-C", dir, "repack", "-a", "-d", "-q").CombinedOutput(); err != nil {
		return fmt.Errorf("repack: %v: %s", err, out)
	}
	headOut, err := exec.CommandContext(ctx, "git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil {
		return fmt.Errorf("rev-parse HEAD: %w", err)
	}
	referee := s.GitReferee()
	st, err := referee.Load(ctx, repoID.String())
	if err != nil {
		return fmt.Errorf("referee load: %w", err)
	}
	m, etag, err := s.gitEngine.LoadManifest(ctx, tenantID.String(), projectSlug, repoSlug)
	if err != nil {
		return fmt.Errorf("load manifest: %w", err)
	}
	rcx := gitengine.ReceiveContext{ID: repoID.String(), Dir: dir, TenantID: tenantID.String(), ProjectSlug: projectSlug, RepoSlug: repoSlug}
	if err := gitengine.SyncRepoToBucket(ctx, s.gitEngine, referee, rcx, m, etag, st, strings.TrimSpace(string(headOut)), actor); err != nil {
		return fmt.Errorf("sync to bucket: %w", err)
	}
	return nil
}

// provisionEngineRepo writes the manifest and the repo + referee rows (the
// CreateRepo engine path). The caller compensates with dropEngineRepo.
func (s *Service) provisionEngineRepo(ctx context.Context, project db.Project, params db.CreateRepositoryParams) (db.Repository, error) {
	if err := s.gitEngine.ProvisionRepo(ctx, project.TenantID.String(), project.Slug, params.Slug, params.DefaultBranch); err != nil {
		if errors.Is(err, objectstore.ErrPreconditionFailed) {
			return db.Repository{}, fmt.Errorf("provision repo: manifest already exists for %s/%s: %w", project.Slug, params.Slug, err)
		}
		return db.Repository{}, fmt.Errorf("provision repo: %w", err)
	}
	var created db.Repository
	err := s.store.InTxTx(ctx, func(tx pgx.Tx) error {
		q := db.New(tx)
		repo, err := q.CreateRepository(ctx, params)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, "INSERT INTO git_repo_state (repo_id, tenant_id) VALUES ($1, $2)", repo.ID, project.TenantID); err != nil {
			return err
		}
		created = repo
		return nil
	})
	if err != nil {
		cctx, cancel := detachedContext(ctx)
		defer cancel()
		if delErr := s.gitEngine.DeleteManifest(cctx, project.TenantID.String(), project.Slug, params.Slug); delErr != nil {
			s.logger.Error("failed to roll back provisioned manifest", "project", project.Slug, "repo", params.Slug, "error", delErr)
		}
		if isUniqueViolation(err) {
			return db.Repository{}, ErrConflict
		}
		return db.Repository{}, err
	}
	return created, nil
}

// dropEngineRepo deletes an engine repo's bucket objects and rows (failed
// seeding compensation and repository deletion).
func (s *Service) dropEngineRepo(ctx context.Context, repo db.Repository, project db.Project) {
	cctx, cancel := detachedContext(ctx)
	defer cancel()
	if _, err := s.gitEngine.DeleteRepoObjects(cctx, project.TenantID.String(), project.Slug, repo.Slug); err != nil {
		s.logger.Warn("could not delete repo objects", "project", project.Slug, "repo", repo.Slug, "error", err)
	}
	if err := s.store.DeleteRepository(cctx, repo.ID); err != nil {
		s.logger.Warn("could not delete repo row", "repo", repo.ID, "error", err)
	}
}

// createSeededEngineRepo provisions an engine repo and fills it from src.
func (s *Service) createSeededEngineRepo(ctx context.Context, project db.Project, params db.CreateRepositoryParams, src seedSource, actor string) (db.Repository, error) {
	dir, branch, err := bareClone(ctx, src)
	if err != nil {
		return db.Repository{}, err
	}
	defer os.RemoveAll(filepath.Dir(dir))
	if branch != "" && validRefName(branch) {
		params.DefaultBranch = branch
	}
	repo, err := s.provisionEngineRepo(ctx, project, params)
	if err != nil {
		return db.Repository{}, err
	}
	if err := s.seedEngineRepo(ctx, repo.ID, project.TenantID, project.Slug, repo.Slug, dir, actor); err != nil {
		s.dropEngineRepo(ctx, repo, project)
		return db.Repository{}, err
	}
	return repo, nil
}

// validRefName is a conservative branch-name check for a cloned HEAD.
func validRefName(b string) bool {
	if b == "" || len(b) > 200 || strings.HasPrefix(b, "-") || strings.Contains(b, "..") {
		return false
	}
	return exec.Command("git", "check-ref-format", "--branch", b).Run() == nil
}

// WithImportURLCheck replaces the import URL policy (tests point imports at a
// loopback git server; production keeps validateImportURL).
func (s *Service) WithImportURLCheck(check func(string) error) *Service {
	s.importURLCheck = check
	return s
}

func (s *Service) checkImportURL(raw string) error {
	if s.importURLCheck != nil {
		return s.importURLCheck(raw)
	}
	return validateImportURL(raw)
}
