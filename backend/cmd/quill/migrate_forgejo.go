package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/nielsuitterdijk22/quill/internal/config"
	"github.com/nielsuitterdijk22/quill/internal/forgejo"
	"github.com/nielsuitterdijk22/quill/internal/gitengine"
	"github.com/nielsuitterdijk22/quill/internal/gitmigrate"
	"github.com/nielsuitterdijk22/quill/internal/logging"
	"github.com/nielsuitterdijk22/quill/internal/objectstore"
	"github.com/nielsuitterdijk22/quill/internal/platform"
	"github.com/nielsuitterdijk22/quill/internal/store"
)

// runForgejoMigrate is the D1 migration runner (serverless epic): moves every
// Forgejo-linked repository into the own-engine bucket truth, repo by repo,
// behind a per-repo read-only window, with a full verification pass (fsck +
// object counts + live-refs comparison) and PR/comment/review history import.
// --dry-run reports without writing anything. Forgejo stays up and serving
// throughout; the bucket copy is additive and every repo is resumable.
func runForgejoMigrate(args []string) error {
	fs := flag.NewFlagSet("migrate-forgejo-to-bucket", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	dryRun := fs.Bool("dry-run", false, "report the per-repo plan (size, refs, PRs), write nothing")
	repoFilter := fs.String("repo", "", "restrict to one repo as project/repo (default: all Forgejo-linked repos)")
	force := fs.Bool("force", false, "re-sync repos the engine already owns")
	skipPRs := fs.Bool("skip-prs", false, "leave pull/comment/review history untouched")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	if !cfg.GitEngine.Own() {
		return errors.New("migrate-forgejo-to-bucket: QUILL_GIT_ENGINE_MODE=own + QUILL_GIT_S3_* are not configured")
	}
	if cfg.Forgejo.BaseURL == "" || cfg.Forgejo.AdminToken == "" {
		return errors.New("migrate-forgejo-to-bucket: FORGEJO_BASE_URL/FORGEJO_ADMIN_TOKEN not configured")
	}
	ctx := context.Background()
	st, err := store.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer st.Close()

	objStore := objectstore.New(objectstore.Config{
		Endpoint:  cfg.GitEngine.S3Endpoint,
		Region:    cfg.GitEngine.S3Region,
		Bucket:    cfg.GitEngine.S3Bucket,
		AccessKey: cfg.GitEngine.S3AccessKey,
		SecretKey: cfg.GitEngine.S3SecretKey,
	})
	engine := gitengine.New(objStore)
	cacheDir := cfg.GitEngine.CacheDir
	if cacheDir == "" {
		cacheDir = filepath.Join(os.TempDir(), "quill-forgejo-migration")
	}
	fc := forgejo.New(cfg.Forgejo)
	repoCache := &gitengine.RepoCache{
		Root:   cacheDir,
		Engine: engine,
		Packs:  gitengine.NewBucketPackSource(objStore),
	}
	svc := platform.NewService(st, fc, logging.New("info", "text")).
		WithGitEngine(engine).
		WithGitCache(repoCache)

	mg := &gitmigrate.Migrator{
		Engine:   engine,
		Cache:    repoCache,
		Leases:   svc.GitPushLeases(),
		Store:    st,
		Referee:  svc.GitReferee(),
		Forgejo:  fc,
		CloneURL: forgejoCloneURL(cfg.Forgejo),
		WorkDir:  filepath.Join(cacheDir, "migration"),
	}

	links, err := st.ListForgejoRepoLinks(ctx)
	if err != nil {
		return fmt.Errorf("list forgejo-linked repos: %w", err)
	}
	if *repoFilter != "" {
		var kept []store.ForgejoRepoLink
		for _, l := range links {
			if l.ProjectSlug+"/"+l.RepoSlug == *repoFilter {
				kept = append(kept, l)
			}
		}
		links = kept
	}
	fmt.Printf("migrate-forgejo-to-bucket: %d repo(s)%s\n", len(links), map[bool]string{true: " (DRY RUN)", false: ""}[*dryRun])

	if *dryRun {
		for _, l := range links {
			p, err := mg.DryRun(ctx, l)
			if err != nil {
				fmt.Printf("  %-40s PLAN FAILED: %v\n", l.ProjectSlug+"/"+l.RepoSlug, err)
				continue
			}
			fmt.Printf("  %-40s %s empty=%v default=%s size=%dKB refs=%d openPRs=%d note=%q\n",
				p.Link.ProjectSlug+"/"+p.Link.RepoSlug, stateWord(p.Link.HasEngineState),
				p.LiveEmpty, p.LiveDefault, p.LiveSize, p.LiveRefs, p.OpenPulls, p.Note)
		}
		return nil
	}

	var migrated, skipped, empty, failed int
	for _, l := range links {
		ident := l.ProjectSlug + "/" + l.RepoSlug
		res, err := mg.MigrateRepo(ctx, l, gitmigrate.Options{
			Force: *force, SkipPRs: *skipPRs,
		})
		switch {
		case err != nil:
			failed++
			fmt.Printf("  %-40s FAILED: %v\n", ident, err)
		case res.Status == "skipped":
			skipped++
			fmt.Printf("  %-40s skipped (%s)\n", ident, res.SkippedReason)
		case res.Status == "empty":
			empty++
			fmt.Printf("  %-40s empty (no refs; empty manifest provisioned)\n", ident)
		default:
			migrated++
			fmt.Printf("  %-40s migrated refs=%d packs=%d bytes=%d prs=%d comments=%d reviews=%d objects=%d liveRefs=%v\n",
				ident, res.Refs, res.Packs, res.StorageBytes, res.PullsImported, res.CommentsAdded, res.ReviewsAdded, res.VerifyObjects, res.LiveRefsVerified)
		}
	}
	fmt.Printf("done: %d repos, %d migrated, %d empty, %d skipped, %d FAILED\n",
		len(links), migrated, empty, skipped, failed)
	if failed > 0 {
		return fmt.Errorf("migrate-forgejo-to-bucket: %d repo(s) failed", failed)
	}
	return nil
}

// forgejoCloneURL builds the HTTPS mirror-clone URL with the admin token.
// The URL embeds a credential — callers pass it only to git, never print it.
func forgejoCloneURL(cfg config.ForgejoConfig) func(owner, name string) string {
	base := strings.TrimSuffix(cfg.BaseURL, "/")
	return func(owner, name string) string {
		return "https://x-access-token:" + url.PathEscape(cfg.AdminToken) + "@" +
			strings.TrimPrefix(base, "https://") + "/" + owner + "/" + name + ".git"
	}
}

func stateWord(engineOwned bool) string {
	if engineOwned {
		return "engine-owned"
	}
	return "forgejo"
}
