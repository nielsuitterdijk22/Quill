package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/nielsuitterdijk22/quill/internal/config"
	"github.com/nielsuitterdijk22/quill/internal/forgejo"
	"github.com/nielsuitterdijk22/quill/internal/logging"
	"github.com/nielsuitterdijk22/quill/internal/platform"
	"github.com/nielsuitterdijk22/quill/internal/store"
)

// runCreateBetaInvite mints a temporary beta signup link (operator tooling):
// whoever opens it signed in gets the free 'beta' plan. The link is printed
// once; only its hash is stored.
func runCreateBetaInvite(args []string) error {
	fs := flag.NewFlagSet("create-beta-invite", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	note := fs.String("note", "", "who the invite is for (shown in the admin list)")
	uses := fs.Int("uses", 1, "how many accounts may redeem it")
	days := fs.Int("days", 14, "how long the link stays valid")
	app := fs.String("app-url", "", "web app origin for the printed link (default: first QUILL_CORS_ALLOWED_ORIGINS entry)")
	if err := fs.Parse(args); err != nil {
		return err
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
	svc := platform.NewService(st, forgejo.New(cfg.Forgejo), logging.New("warn", "text"))
	inv, code, err := svc.CreateBetaInvite(ctx, platform.Actor{IsAdmin: true}, *note, *uses, *days)
	if err != nil {
		return err
	}
	origin := strings.TrimRight(*app, "/")
	if origin == "" && len(cfg.CORSAllowedOrigins) > 0 {
		origin = strings.TrimRight(cfg.CORSAllowedOrigins[0], "/")
	}
	if origin == "" {
		return errors.New("create-beta-invite: pass --app-url (no web origin configured)")
	}
	fmt.Printf("beta invite for %q: %d use(s), valid until %s\n%s/beta/%s\n", inv.Note, inv.MaxUses, inv.ExpiresAt.Format("2006-01-02 15:04 MST"), origin, code)
	return nil
}
