package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/nielsuitterdijk22/quill/internal/config"
	"github.com/nielsuitterdijk22/quill/internal/forgejo"
	"github.com/nielsuitterdijk22/quill/internal/gitengine"
	"github.com/nielsuitterdijk22/quill/internal/logging"
	"github.com/nielsuitterdijk22/quill/internal/objectstore"
	"github.com/nielsuitterdijk22/quill/internal/platform"
	"github.com/nielsuitterdijk22/quill/internal/store"
)

// runGitMaintenance is the B7 async-maintenance runner (serverless epic): it
// walks every own-engine repo, reconciles the manifest against the Postgres
// referee (the repair for pushes whose manifest write lost its etag race),
// repacks past --repack-threshold, and garbage-collects orphaned pack pairs
// behind a lease + a cold-scratch fsck. Idempotent by construction; run via
// cron/Serverless Job — never inside a request path. Failures are per repo:
// one broken repo never stops the walk.
func runGitMaintenance(args []string) error {
	fs := flag.NewFlagSet("git-maintenance", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	repackThreshold := fs.Int("repack-threshold", 0, "consolidate into one pack when a repo lists more than N packs (0 = never)")
	repoFilter := fs.String("repo", "", "restrict to one repo as project/repo (default: all engine repos)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	if !cfg.GitEngine.Own() {
		return errors.New("git-maintenance: QUILL_GIT_ENGINE_MODE=own is not configured")
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
		cacheDir = filepath.Join(os.TempDir(), "quill-git-maintenance")
	}
	svc := platform.NewService(st, forgejo.New(cfg.Forgejo), logging.New("info", "text")).
		WithGitEngine(engine).
		WithGitCache(&gitengine.RepoCache{
			Root:   cacheDir,
			Engine: engine,
			Packs:  gitengine.NewBucketPackSource(objStore),
		})
	mn := svc.GitMaintenance()

	repos, err := svc.ListEngineRepos(ctx)
	if err != nil {
		return fmt.Errorf("list engine repos: %w", err)
	}
	fmt.Printf("git-maintenance: %d engine repo(s)\n", len(repos))

	var repaired, repacked, orphaned, skipped, failed int
	for _, ref := range repos {
		ident := ref.ProjectSlug + "/" + ref.RepoSlug
		if *repoFilter != "" && ident != *repoFilter {
			continue
		}
		lnk, err := mn.Maintain(ctx, gitengine.ReceiveContext{
			ID:          ref.RepoID.String(),
			TenantID:    ref.TenantID.String(),
			ProjectSlug: ref.ProjectSlug,
			RepoSlug:    ref.RepoSlug,
		}, gitengine.MaintenanceOptions{RepackThreshold: *repackThreshold})
		switch {
		case err != nil:
			failed++
			fmt.Printf("  %-40s FAILED: %v\n", ident, err)
			continue
		case lnk.LeaseHeld:
			skipped++
			fmt.Printf("  %-40s skipped (lease held)\n", ident)
			continue
		}
		if lnk.ManifestRepaired {
			repaired++
		}
		if lnk.RepackedTo > 0 {
			repacked++
		}
		orphaned += lnk.OrphanPacksDeleted
		fmt.Printf("  %-40s ok (repaired=%v adopted=%d repackedTo=%d orphansDeleted=%d)\n",
			ident, lnk.ManifestRepaired, lnk.PacksAdopted, lnk.RepackedTo, lnk.OrphanPacksDeleted)
	}
	fmt.Printf("done: %d repos, %d repaired, %d repacked, %d orphan packs deleted, %d lease-skipped, %d FAILED\n",
		len(repos), repaired, repacked, orphaned, skipped, failed)
	if failed > 0 {
		return fmt.Errorf("git-maintenance: %d repo(s) failed", failed)
	}
	return nil
}
