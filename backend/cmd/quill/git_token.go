package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/nielsuitterdijk22/quill/internal/config"
	"github.com/nielsuitterdijk22/quill/internal/forgejo"
	"github.com/nielsuitterdijk22/quill/internal/logging"
	"github.com/nielsuitterdijk22/quill/internal/platform"
	"github.com/nielsuitterdijk22/quill/internal/store"
)

// runIssueGitToken mints a git token for a user (operator tooling: an agent
// pushing on the owner's behalf). The secret is written to --out (mode 0600),
// never printed; it shows up in the user's token list under --name and can
// be revoked there. Delete the file after use.
func runIssueGitToken(args []string) error {
	fs := flag.NewFlagSet("issue-git-token", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	username := fs.String("username", "", "user to mint the token for")
	name := fs.String("name", "operator", "token label shown in the user's token list")
	out := fs.String("out", "", "file to write \"<username>:<token>\" to (created 0600)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *username == "" || *out == "" {
		return errors.New("issue-git-token: --username and --out are required")
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
	user, err := st.GetUserByUsername(ctx, *username)
	if err != nil {
		return fmt.Errorf("user %s: %w", *username, err)
	}
	svc := platform.NewService(st, forgejo.New(cfg.Forgejo), logging.New("warn", "text"))
	cred, err := svc.MintGitToken(ctx, platform.Actor{UserID: user.ID}, *name)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(*out, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := fmt.Fprintf(f, "%s:%s\n", cred.Username, cred.Token); err != nil {
		return err
	}
	fmt.Printf("git token %q minted for %s (id %s) → %s\n", *name, cred.Username, cred.ID, *out)
	return nil
}
