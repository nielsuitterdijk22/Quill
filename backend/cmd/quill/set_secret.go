package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/nielsuitterdijk22/quill/internal/config"
	"github.com/nielsuitterdijk22/quill/internal/forgejo"
	"github.com/nielsuitterdijk22/quill/internal/logging"
	"github.com/nielsuitterdijk22/quill/internal/platform"
	"github.com/nielsuitterdijk22/quill/internal/secretbox"
	"github.com/nielsuitterdijk22/quill/internal/store"
)

// runSetRepoSecret sets a repository pipeline secret from stdin, acting as
// the project's owner — the operator path for wiring CI credentials (e.g. the
// deploy pipeline's Scaleway key) without anyone's password. The value never
// appears in argv, the environment or output. Requires the production
// QUILL_SECRET_ENCRYPTION_KEY (the dev cipher is refused).
func runSetRepoSecret(args []string) error {
	fs := flag.NewFlagSet("set-repo-secret", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	project := fs.String("project", "", "project slug")
	repo := fs.String("repo", "", "repository slug")
	name := fs.String("name", "", "secret name")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *project == "" || *repo == "" || *name == "" {
		return errors.New("set-repo-secret: --project, --repo and --name are required; the value is read from stdin")
	}
	raw, err := io.ReadAll(bufio.NewReader(io.LimitReader(os.Stdin, 64<<10)))
	if err != nil {
		return err
	}
	value := strings.TrimRight(string(raw), "\r\n")
	if value == "" {
		return errors.New("set-repo-secret: empty value on stdin")
	}
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	cipher, err := secretbox.NewFromBase64Key(cfg.SecretEncryptionKey)
	if err != nil {
		return fmt.Errorf("set-repo-secret: QUILL_SECRET_ENCRYPTION_KEY: %w", err)
	}
	ctx := context.Background()
	st, err := store.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer st.Close()
	svc := platform.NewService(st, forgejo.New(cfg.Forgejo), logging.New("warn", "text")).WithCipher(cipher)
	owner, err := svc.ProjectOwner(ctx, *project)
	if err != nil {
		return err
	}
	sum, err := svc.SetRepoSecret(ctx, platform.Actor{UserID: owner}, *project, *repo, *name, value)
	if err != nil {
		return err
	}
	fmt.Printf("secret %s set on %s/%s (updated %s)\n", sum.Name, *project, *repo, sum.UpdatedAt.Format("2006-01-02 15:04"))
	return nil
}
