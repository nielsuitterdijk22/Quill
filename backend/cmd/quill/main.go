// Command quill is the Quill administrative CLI. It operates on the platform
// directly (Postgres + Forgejo) without going through the HTTP API, for
// bootstrapping and repair tasks such as creating tenants and repositories.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/nielsuitterdijk22/quill/internal/config"
	"github.com/nielsuitterdijk22/quill/internal/forgejo"
	"github.com/nielsuitterdijk22/quill/internal/store"
)

func main() {
	log.SetFlags(0)
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return usage()
	}
	switch args[0] {
	case "admin":
		return runAdmin(args[1:])
	case "-h", "--help", "help":
		return usage()
	default:
		return fmt.Errorf("unknown command %q (see --help)", args[0])
	}
}

func usage() error {
	fmt.Fprint(os.Stderr, `quill - Quill administrative CLI

Usage:
  quill admin create-repo --tenant <id-or-slug> --name <name> [--description <text>] [--visibility public|internal|private]

Commands:
  admin create-repo  create a repository in the tenant's Forgejo org and record it in Postgres

Configuration is read from the environment (same variables as the API):
  QUILL_DATABASE_URL   Postgres DSN (default: postgres://quill:quill@localhost:5432/quill?sslmode=disable)
  FORGEJO_BASE_URL     Forgejo base URL (default: http://localhost:3000)
  FORGEJO_ADMIN_TOKEN  Forgejo admin access token
  FORGEJO_PUBLIC_URL   externally-accessible Forgejo URL used in the printed URL (defaults to FORGEJO_BASE_URL)
`)
	return nil
}

func runAdmin(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("admin: missing subcommand (create-repo)")
	}
	switch args[0] {
	case "create-repo":
		return runCreateRepo(args[1:])
	default:
		return fmt.Errorf("admin: unknown subcommand %q (create-repo)", args[0])
	}
}

func runCreateRepo(args []string) error {
	fs := flag.NewFlagSet("create-repo", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	tenant := fs.String("tenant", "", "tenant ID (UUID) or slug the repository belongs to (required)")
	name := fs.String("name", "", "repository name (required)")
	description := fs.String("description", "", "repository description")
	visibility := fs.String("visibility", "private", "repository visibility: public, internal or private")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *name == "" {
		return fmt.Errorf("create-repo: --name is required")
	}
	if *tenant == "" {
		return fmt.Errorf("create-repo: --tenant is required")
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
	fj := forgejo.New(cfg.Forgejo)

	url, err := createRepo(ctx, newStoreAdapter(st), fj, cfg.Forgejo.PublicURL, createRepoInput{
		TenantRef:   *tenant,
		Name:        *name,
		Description: *description,
		Visibility:  *visibility,
	})
	if err != nil {
		return err
	}
	fmt.Println(url)
	return nil
}
