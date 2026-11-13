package gitmigrate

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"github.com/nielsuitterdijk22/quill/internal/forgejo"
	"github.com/nielsuitterdijk22/quill/internal/gitengine"
	"github.com/nielsuitterdijk22/quill/internal/objectstore"
	"github.com/nielsuitterdijk22/quill/internal/store"
)

// PR D1 (serverless epic): the Forgejo → bucket migration tool. Per repo it
// mirrors the Forgejo truth (clone --mirror), repacks into one pack, and
// commits the engine truth through the SAME primitives a push uses —
// SyncRepoToBucket (refs snapshot, pack uploads, referee CAS FIRST, manifest
// If-Match view SECOND) — plus the PR/comment/review history into the B5
// Postgres model with original numbers, authors and timestamps. The repo is
// FROZEN on Forgejo (branch protection `*`, enable_push=false) BEFORE the
// clone: the read-only window spans the whole migrate+verify run, and the
// freeze makes post-clone divergence impossible. Verify then compares the
// migrated view against LIVE Forgejo (ls-remote) and a scratch hydration
// (fsck + object-count equality).
//
// Idempotent per repo: engine-owned repos (non-empty referee refs) are
// skipped unless Force; history imports are no-ops on existing rows (UNIQUE
// (repo_id, number)). Forgejo stays up and serving the whole time; the bucket
// copy is purely additive. Rollback: delete the repo's manifest + referee row
// + imported pull rows and unfreeze — or simply keep the repo flagged back to
// Forgejo: the forgejo_* columns die in D2, not here.

// Result is one repo's migration outcome.
type Result struct {
	Link          store.ForgejoRepoLink
	Status        string // "migrated" | "skipped" | "empty"
	Refs          int
	Packs         int
	StorageBytes  int64
	PullsImported int
	CommentsAdded int
	ReviewsAdded  int
	SkippedReason string
	// Verify section — only for migrated repos.
	VerifyObjects int
	// LiveRefsVerified is true when every ref ls-remote'd from Forgejo equals
	// the manifest target (the divergence check for pushes that raced the
	// freeze window).
	LiveRefsVerified bool
}

// Options tunes one MigrateRepo run.
type Options struct {
	// Force re-migrates a repo the engine already owns (re-sync).
	Force bool
	// SkipPRs leaves pull/comment/review history untouched.
	SkipPRs bool
	// NoFreeze skips the Forgejo branch-protection freeze (tests).
	NoFreeze bool
	// Holder is the push-lease holder identity (tests make failures visible).
	Holder string
}

// Migrator runs migrations. Every engine dependency is injected: tests use
// the MemStore bucket, a local fixture bare repo as the "Forgejo" source and
// the test-DB referee; production wires exactly the adapters the /git mount
// uses (pgReferee, push_leases, the api container's S3 client).
type Migrator struct {
	Engine *gitengine.Engine
	Cache  *gitengine.RepoCache
	Leases gitengine.PushLeases
	Store  *store.Store
	// Referee is the CAS authority (pgReferee in production).
	Referee gitengine.Referee
	// Forgejo is the mirror source client (nil in tests that freeze nothing).
	Forgejo *forgejo.Client
	// CloneURL builds the mirror-clone URL for owner/name. Tests hand back a
	// file:// path to a fixture bare repo.
	CloneURL func(owner, name string) string
	// WorkDir is the scratch root for mirror clones + verify hydrations.
	WorkDir string
}

// runGit runs `git` with no terminal prompts and a pinned identity (commit
// plumbing never needs a global git config, CI runners have none).
func runGit(dir string, args ...string) ([]byte, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_TERMINAL_PROMPT=0",
		"GIT_AUTHOR_NAME=forgejo-migration", "GIT_AUTHOR_EMAIL=migration@quill",
		"GIT_COMMITTER_NAME=forgejo-migration", "GIT_COMMITTER_EMAIL=migration@quill")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return out, fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return out, nil
}

// FreezeRepo sets a repo-wide read-only window on Forgejo: a `*` branch
// protection with enable_push=false blocks direct pushes to every branch.
// The rule is additive — a repo's real Quill-mirrored protections stay
// untouched, and the `*` rule is exactly what cutover removes.
func (mg *Migrator) FreezeRepo(ctx context.Context, owner, name string) error {
	if mg.Forgejo == nil {
		return nil
	}
	return mg.Forgejo.UpsertBranchProtection(ctx, owner, name, forgejo.BranchProtectionOptions{
		BranchName: "*",
		EnablePush: false,
	})
}

// UnfreezeRepo removes the migration freeze (the `*` rule). Missing rule = ok.
func (mg *Migrator) UnfreezeRepo(ctx context.Context, owner, name string) error {
	if mg.Forgejo == nil {
		return nil
	}
	return mg.Forgejo.DeleteBranchProtection(ctx, owner, name, "*")
}

// MigrateRepo migrates one linked repository. A nil error with Status
// "skipped" means idempotent nothing-to-do.
func (mg *Migrator) MigrateRepo(ctx context.Context, link store.ForgejoRepoLink, opts Options) (Result, error) {
	res := Result{Link: link}
	if link.TenantID == nil {
		res.Status = "skipped"
		res.SkippedReason = "no tenant stamp (legacy row): tenant-scoped layout unaddressable"
		return res, nil
	}
	tenantID := *link.TenantID

	// Engine-owned already? Skip unless Force (re-sync re-commits the same
	// refs — SyncRepoToBucket's refsEqual short-circuits to a no-op).
	// Forgejo-era repos were never provisioned through the engine and carry
	// no referee row yet: create it (tenant-stamped, idempotent — an existing
	// row keeps its refs and revision) before the first Load.
	if mg.Store != nil {
		if _, err := mg.Store.EnsureGitRepoState(ctx, link.RepoID, tenantID); err != nil {
			return res, fmt.Errorf("ensure referee row: %w", err)
		}
	}
	st, err := mg.Referee.Load(ctx, link.RepoID.String())
	if err != nil {
		return res, fmt.Errorf("referee load: %w", err)
	}
	if len(st.Refs) > 0 && !opts.Force {
		res.Status = "skipped"
		res.SkippedReason = fmt.Sprintf("engine-owned (revision %d, %d refs)", st.Revision, len(st.Refs))
		return res, nil
	}

	holder := opts.Holder
	if holder == "" {
		holder = "forgejo-migration-" + uuid.NewString()[:8]
	}
	release, err := mg.Leases.Acquire(ctx, link.RepoID.String(), gitengine.PushLeaseTTL)
	if err != nil {
		return res, fmt.Errorf("push lease: %w", err)
	}
	defer func() { _ = release() }()

	// Freeze FIRST: the read-only window spans clone + migrate + verify.
	if !opts.NoFreeze {
		if err := mg.FreezeRepo(ctx, link.ForgejoOwner, link.ForgejoName); err != nil {
			return res, fmt.Errorf("freeze (branch protection *): %w", err)
		}
	}

	dir, err := mg.mirrorClone(link)
	if err != nil {
		return res, err
	}

	// Snapshot refs + default branch (a mirror clone preserves HEAD's symref,
	// which IS Forgejo's default branch — the heuristic).
	refs, err := gitengine.RefSnapshot(dir)
	if err != nil {
		return res, err
	}
	defaultBranch := link.DefaultBranch
	if out, err := runGit(dir, "symbolic-ref", "--short", "HEAD"); err == nil {
		if b := strings.TrimSpace(string(out)); b != "" {
			defaultBranch = b
		}
	}
	if len(refs) == 0 {
		// Empty mirror: write the empty manifest create-if-absent exactly the
		// way ProvisionRepo does and commit the empty referee state. A push to
		// the engine later takes the normal create path.
		if err := mg.Engine.ProvisionRepo(ctx, tenantID.String(), link.ProjectSlug, link.RepoSlug, defaultBranch); err != nil {
			return res, fmt.Errorf("provision empty repo: %w", err)
		}
		if _, err := mg.Referee.Commit(ctx, link.RepoID.String(), st, map[string]string{}, 0); err != nil {
			return res, fmt.Errorf("referee commit (empty): %w", err)
		}
		res.Status = "empty"
		return res, nil
	}
	res.Refs = len(refs)

	// One pack: the bucket layout contract is whole packs, individually
	// addressable — mirror clones arrive already packed, repack -a -d
	// guarantees a single consolidated pack (or none for empty repos).
	if _, err := runGit(dir, "repack", "-a", "-d"); err != nil {
		return res, fmt.Errorf("repack: %w", err)
	}
	head, err := runGit(dir, "rev-parse", "HEAD")
	if err != nil {
		return res, fmt.Errorf("rev-parse HEAD: %w", err)
	}

	m, etag, err := mg.Engine.LoadManifest(ctx, tenantID.String(), link.ProjectSlug, link.RepoSlug)
	if err != nil {
		if !errors.Is(err, objectstore.ErrNotFound) {
			return res, fmt.Errorf("load manifest: %w", err)
		}
		m = gitengine.NewManifest(tenantID.String(), defaultBranch)
		etag = ""
	}

	rcx := gitengine.ReceiveContext{
		ID:          link.RepoID.String(),
		Dir:         dir,
		TenantID:    tenantID.String(),
		ProjectSlug: link.ProjectSlug,
		RepoSlug:    link.RepoSlug,
	}
	if err := gitengine.SyncRepoToBucket(ctx, mg.Engine, mg.Referee, rcx, m, etag, st, strings.TrimSpace(string(head)), "forgejo-import"); err != nil {
		return res, fmt.Errorf("sync to bucket: %w", err)
	}

	// Re-read the committed view for the report + verification.
	m, _, err = mg.Engine.LoadManifest(ctx, tenantID.String(), link.ProjectSlug, link.RepoSlug)
	if err != nil {
		return res, fmt.Errorf("reload manifest: %w", err)
	}
	res.Status = "migrated"
	res.Refs = len(m.Refs)
	res.Packs = len(m.Packs)
	res.StorageBytes = m.StorageBytes

	if !opts.SkipPRs {
		if err := mg.importPullHistory(ctx, link, tenantID, &res); err != nil {
			return res, fmt.Errorf("pr history: %w", err)
		}
	}

	if err := mg.verify(ctx, link, tenantID, m, dir, &res); err != nil {
		return res, fmt.Errorf("verify: %w", err)
	}
	return res, nil
}

// mirrorClone clones --mirror into the scratch tree.
func (mg *Migrator) mirrorClone(link store.ForgejoRepoLink) (string, error) {
	dir := filepath.Join(mg.WorkDir, "clone", link.ProjectSlug, link.RepoSlug)
	_ = os.RemoveAll(dir)
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return "", err
	}
	if _, err := runGit("", "clone", "--mirror", "--quiet", mg.CloneURL(link.ForgejoOwner, link.ForgejoName), dir); err != nil {
		return "", fmt.Errorf("mirror clone %s/%s: %w", link.ForgejoOwner, link.ForgejoName, err)
	}
	return dir, nil
}

// importPullHistory copies Forgejo's PR/comment/review/line-comment history
// into the B5 model, preserving numbers, authors (resolved to Quill users
// when possible; the name snapshot is always kept) and timestamps.
// Chronological order preserves the seq ordering the DTOs render by.
func (mg *Migrator) importPullHistory(ctx context.Context, link store.ForgejoRepoLink, tenantID uuid.UUID, res *Result) error {
	owner, name := link.ForgejoOwner, link.ForgejoName
	pulls, err := mg.Forgejo.ListPullsAll(ctx, owner, name, "all")
	if err != nil {
		return fmt.Errorf("list pulls: %w", err)
	}
	for _, pr := range pulls {
		createdByID, _, err := mg.resolveUser(ctx, authorLogin(pr.User))
		if err != nil {
			return err
		}
		mergedByID, mergedByName, _ := mg.resolveUser(ctx, authorLogin(pr.MergedBy))
		// Forgejo exposes no merge method on read; merged history keeps the
		// empty default. Closed-not-merged PRs keep state closed, merged=false.
		imported, err := mg.Store.ImportPull(ctx, link.RepoID, tenantID, pr.Number,
			pr.Title, pr.Body,
			pr.Head.Ref, pr.Head.SHA, pr.Base.Ref, pr.Base.SHA, pr.State,
			pr.Merged, mergedByID, mergedByName, pr.MergedAt, pr.MergeCommit,
			createdByID, authorLogin(pr.User), pr.CreatedAt)
		if err != nil {
			return fmt.Errorf("import pull #%d: %w", pr.Number, err)
		}
		res.PullsImported++

		comments, err := mg.Forgejo.ListIssueComments(ctx, owner, name, pr.Number)
		if err != nil {
			return fmt.Errorf("comments #%d: %w", pr.Number, err)
		}
		for _, c := range comments {
			aid, _, err := mg.resolveUser(ctx, authorLogin(c.User))
			if err != nil {
				return err
			}
			if err := mg.Store.ImportPullComment(ctx, imported.ID, tenantID, aid, authorLogin(c.User), c.Body, c.CreatedAt); err != nil {
				return fmt.Errorf("import comment #%d: %w", pr.Number, err)
			}
			res.CommentsAdded++
		}

		reviews, err := mg.Forgejo.ListReviews(ctx, owner, name, pr.Number)
		if err != nil {
			return fmt.Errorf("reviews #%d: %w", pr.Number, err)
		}
		for _, r := range reviews {
			// PENDING reviews are invisible drafts on Forgejo; migrating them
			// would publish what their author never submitted.
			if r.State == forgejo.ReviewPending {
				continue
			}
			rid, _, err := mg.resolveUser(ctx, authorLogin(r.User))
			if err != nil {
				return err
			}
			if err := mg.Store.ImportPullReview(ctx, imported.ID, tenantID, rid, authorLogin(r.User), r.State, r.Body, r.SubmittedAt); err != nil {
				return fmt.Errorf("import review #%d: %w", pr.Number, err)
			}
			res.ReviewsAdded++
			lineComments, err := mg.Forgejo.ListReviewComments(ctx, owner, name, pr.Number, r.ID)
			if err != nil {
				return fmt.Errorf("review comments #%d: %w", pr.Number, err)
			}
			for _, lc := range lineComments {
				lid, _, err := mg.resolveUser(ctx, authorLogin(lc.User))
				if err != nil {
					return err
				}
				// Line anchors on the newer API; older deployments only fill
				// Position. 0 = file-level comment.
				line := lc.Line
				if line == 0 {
					line = lc.Position
				}
				if err := mg.Store.ImportPullLineComment(ctx, imported.ID, tenantID, lid, authorLogin(lc.User), lc.Path, line, lc.Body, lc.CreatedAt); err != nil {
					return fmt.Errorf("import line comment #%d: %w", pr.Number, err)
				}
			}
		}
	}
	return nil
}

// resolveUser maps a Forgejo login to a Quill user id (nil when unknown —
// history import NEVER blocks on identity resolution; the name snapshot
// keeps rows renderable).
func (mg *Migrator) resolveUser(ctx context.Context, login string) (*uuid.UUID, string, error) {
	if login == "" {
		return nil, "", nil
	}
	id, found, err := mg.Store.FindUserForForgejoLogin(ctx, login)
	if err != nil {
		return nil, "", fmt.Errorf("resolve forgejo login %q: %w", login, err)
	}
	if !found {
		return nil, login, nil
	}
	return &id, login, nil
}

func authorLogin(u *forgejo.User) string {
	if u == nil {
		return ""
	}
	return u.Login
}

// verify proves the migrated copy serves the same repository Forgejo does:
// (1) a scratch hydration from the bucket → fsck --connectivity-only and
// rev-list --objects count equality with the clone; (2) every ref from LIVE
// Forgejo (ls-remote) equals the manifest target — the check that catches a
// push that raced the freeze.
func (mg *Migrator) verify(ctx context.Context, link store.ForgejoRepoLink, tenantID uuid.UUID, m *gitengine.Manifest, cloneDir string, res *Result) error {
	hyd, err := mg.Cache.Hydrate(ctx, tenantID.String(), link.ProjectSlug, link.RepoSlug)
	if err != nil {
		return fmt.Errorf("scratch hydration: %w", err)
	}
	if _, err := runGit(hyd, "fsck", "--connectivity-only"); err != nil {
		return fmt.Errorf("hydrated fsck: %w", err)
	}
	nHyd, err := countObjects(hyd)
	if err != nil {
		return err
	}
	nClone, err := countObjects(cloneDir)
	if err != nil {
		return err
	}
	if nHyd != nClone {
		return fmt.Errorf("object count mismatch: hydrated %d vs clone %d", nHyd, nClone)
	}
	res.VerifyObjects = nHyd

	live, err := mg.liveRefs(link)
	if err != nil {
		return err
	}
	manifestMap := map[string]string{}
	for _, r := range m.Refs {
		manifestMap[r.Name] = r.Target
	}
	verified := len(live) == len(manifestMap)
	for name, target := range live {
		if manifestMap[name] != target {
			verified = false
			break
		}
	}
	res.LiveRefsVerified = verified
	if !verified {
		return errors.New("live refs diverge from the migrated manifest (a push landed during the window)")
	}
	return nil
}

// liveRefs ls-remotes the live Forgejo URL, keeping branch+tag refs only:
// Forgejo's pull head refs are excluded from the manifest on purpose (pull
// state lives in the PR model, not the refs view).
func (mg *Migrator) liveRefs(link store.ForgejoRepoLink) (map[string]string, error) {
	out, err := runGit("", "ls-remote", mg.CloneURL(link.ForgejoOwner, link.ForgejoName))
	if err != nil {
		return nil, fmt.Errorf("ls-remote: %w", err)
	}
	refs := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		if !strings.HasPrefix(fields[1], "refs/heads/") && !strings.HasPrefix(fields[1], "refs/tags/") {
			continue
		}
		if strings.HasSuffix(fields[1], "^{}") {
			continue // peeled tag duplicate line: the manifest carries Peeled
		}
		refs[fields[1]] = fields[0]
	}
	return refs, nil
}

func countObjects(dir string) (int, error) {
	out, err := runGit(dir, "rev-list", "--objects", "--all")
	if err != nil {
		return 0, err
	}
	s := strings.TrimSpace(string(out))
	if s == "" {
		return 0, nil
	}
	return len(strings.Split(s, "\n")), nil
}

// Plan is one repo's dry-run report; nothing is written.
type Plan struct {
	Link        store.ForgejoRepoLink
	LiveEmpty   bool
	LiveDefault string
	LiveSize    int64
	LiveRefs    int
	OpenPulls   int
	Note        string
}

// DryRun reports one repo without writing anything: live size/emptiness from
// Forgejo, live ref count via ls-remote, and the open-PR count the import
// would carry.
func (mg *Migrator) DryRun(ctx context.Context, link store.ForgejoRepoLink) (Plan, error) {
	p := Plan{Link: link}
	if mg.Forgejo == nil {
		p.Note = "no forgejo client"
		return p, nil
	}
	repo, err := mg.Forgejo.GetRepo(ctx, link.ForgejoOwner, link.ForgejoName)
	if err != nil {
		return p, fmt.Errorf("get repo %s/%s: %w", link.ForgejoOwner, link.ForgejoName, err)
	}
	p.LiveEmpty = repo.Empty
	p.LiveDefault = repo.DefaultBranch
	p.LiveSize = repo.Size
	if out, err := runGit("", "ls-remote", mg.CloneURL(link.ForgejoOwner, link.ForgejoName)); err == nil {
		if s := strings.TrimSpace(string(out)); s != "" {
			p.LiveRefs = len(strings.Split(s, "\n"))
		}
	}
	if opens, err := mg.Forgejo.ListPullsAll(ctx, link.ForgejoOwner, link.ForgejoName, "open"); err == nil {
		p.OpenPulls = len(opens)
	}
	if link.HasEngineState {
		p.Note = "already engine-owned (skipped on migrate)"
	}
	return p, nil
}
