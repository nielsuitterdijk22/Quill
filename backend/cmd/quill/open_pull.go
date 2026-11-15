package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/nielsuitterdijk22/quill/internal/config"
	"github.com/nielsuitterdijk22/quill/internal/store"
	"github.com/nielsuitterdijk22/quill/internal/store/db"
)

// runOpenPull opens a pull request on an engine repo as a user (operator
// tooling: agents push a branch with a git token and open its PR here; they
// have no API token). The branch must already be pushed; --head-sha and
// --base-sha are the tips as the caller pushed them (`git rev-parse`). An open
// PR re-reads its branch tips on every view, so they only seed the row.
func runOpenPull(args []string) error {
	fs := flag.NewFlagSet("open-pull", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	project := fs.String("project", "", "project slug")
	repo := fs.String("repo", "", "repository slug")
	author := fs.String("author", "", "username the PR is opened as")
	head := fs.String("head", "", "source branch")
	base := fs.String("base", "main", "target branch")
	headSHA := fs.String("head-sha", "", "source branch tip")
	baseSHA := fs.String("base-sha", "", "target branch tip")
	title := fs.String("title", "", "PR title")
	bodyFile := fs.String("body-file", "", "file holding the PR description (markdown)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *project == "" || *repo == "" || *author == "" || *head == "" || *headSHA == "" || *baseSHA == "" || strings.TrimSpace(*title) == "" {
		return errors.New("open-pull: --project, --repo, --author, --head, --head-sha, --base-sha and --title are required")
	}
	body := ""
	if *bodyFile != "" {
		b, err := os.ReadFile(*bodyFile)
		if err != nil {
			return err
		}
		body = strings.TrimRight(string(b), " \t\r\n")
	}
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	ctx := context.Background()
	st, err := store.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer st.Close()
	user, err := st.GetUserByUsername(ctx, *author)
	if err != nil {
		return fmt.Errorf("user %s: %w", *author, err)
	}
	p, err := st.GetProjectBySlug(ctx, strings.ToLower(*project))
	if err != nil {
		return fmt.Errorf("project %s: %w", *project, err)
	}
	r, err := st.GetRepositoryBySlug(ctx, db.GetRepositoryBySlugParams{ProjectID: p.ID, Lower: strings.ToLower(*repo)})
	if err != nil {
		return fmt.Errorf("repo %s/%s: %w", *project, *repo, err)
	}
	pr, err := st.CreatePull(ctx, r.ID, p.TenantID, user.ID, user.Username, strings.TrimSpace(*title), body, *head, *headSHA, *base, *baseSHA)
	if err != nil {
		return err
	}
	fmt.Printf("opened pull request #%d on %s/%s (%s → %s)\n", pr.Number, *project, *repo, *head, *base)
	return nil
}
