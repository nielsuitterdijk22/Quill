package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/nielsuitterdijk22/quill/internal/config"
	"github.com/nielsuitterdijk22/quill/internal/gitengine"
	"github.com/nielsuitterdijk22/quill/internal/objectstore"
	"github.com/nielsuitterdijk22/quill/internal/store"
)

// runGitRelocate repairs engine repos whose project was renamed without their
// bucket objects moving (2026-11-14: a username change renamed the personal
// project, leaving its repos' packs under the old slug). For every engine
// repo of --project whose manifest is missing at the current prefix but
// present under --from-project, it copies the objects across. The old copy
// is kept (delete it by hand once verified). Dry run unless --confirm.
func runGitRelocate(args []string) error {
	fs := flag.NewFlagSet("git-relocate", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	project := fs.String("project", "", "current project slug")
	from := fs.String("from-project", "", "project slug the objects still live under")
	confirm := fs.Bool("confirm", false, "copy (default: report only)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *project == "" || *from == "" {
		return errors.New("git-relocate: --project and --from-project are required")
	}
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	if !cfg.GitEngine.Own() {
		return errors.New("git-relocate: QUILL_GIT_ENGINE_MODE=own is not configured")
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
	repos, err := st.ListEngineRepos(ctx)
	if err != nil {
		return err
	}
	for _, r := range repos {
		if r.ProjectSlug != *project {
			continue
		}
		tenant := r.TenantID.String()
		_, _, curErr := objStore.Head(ctx, gitengine.ManifestKey(tenant, r.ProjectSlug, r.RepoSlug))
		_, _, oldErr := objStore.Head(ctx, gitengine.ManifestKey(tenant, *from, r.RepoSlug))
		switch {
		case curErr == nil:
			fmt.Printf("  %s/%s: ok (manifest present)\n", r.ProjectSlug, r.RepoSlug)
		case oldErr != nil:
			fmt.Printf("  %s/%s: MISSING, and not under %s either\n", r.ProjectSlug, r.RepoSlug, *from)
		case !*confirm:
			fmt.Printf("  %s/%s: would copy from %s/%s\n", r.ProjectSlug, r.RepoSlug, *from, r.RepoSlug)
		default:
			n, err := engine.CopyRepoObjects(ctx, tenant, *from, r.RepoSlug, r.ProjectSlug, r.RepoSlug)
			if err != nil {
				return fmt.Errorf("%s/%s: %w", r.ProjectSlug, r.RepoSlug, err)
			}
			fmt.Printf("  %s/%s: copied %d object(s) from %s/%s\n", r.ProjectSlug, r.RepoSlug, n, *from, r.RepoSlug)
		}
	}
	return nil
}
