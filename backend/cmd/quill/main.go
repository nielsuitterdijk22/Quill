// Command quill is the Quill administrative CLI. It operates on the platform
// directly (Postgres + Forgejo) without going through the HTTP API, for
// bootstrapping and repair tasks such as creating tenants and repositories.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
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
  quill admin seed-file --tenant <id-or-slug> --repo <name> --path <repo-relative-path> --content <text> [--branch <ref>]
  quill admin seed-file --tenant <id-or-slug> --repo <name> --path <repo-relative-path> --file <path-or--> [--branch <ref>]

Commands:
  admin create-repo  create a repository in the tenant's Forgejo org and record it in Postgres
  admin seed-file    create or update a file in a repository via the Forgejo API, committing as an admin seed

seed-file content comes from --content (inline text) or --file (a file path, or '-' to read stdin).

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
		return fmt.Errorf("admin: missing subcommand (create-repo, seed-file)")
	}
	switch args[0] {
	case "create-repo":
		return runCreateRepo(args[1:])
	case "seed-file":
		return runSeedFile(args[1:])
	default:
		return fmt.Errorf("admin: unknown subcommand %q (create-repo, seed-file)", args[0])
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

func runSeedFile(args []string) error {
	fs := flag.NewFlagSet("seed-file", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	tenant := fs.String("tenant", "", "tenant ID (UUID) or slug the repository belongs to (required)")
	repo := fs.String("repo", "", "repository name (required)")
	path := fs.String("path", "", "repository-relative file path (required)")
	content := fs.String("content", "", "file content (inline text)")
	file := fs.String("file", "", "path to a file to read as the content ('-' reads stdin)")
	branch := fs.String("branch", "", "branch to commit to (default: the repository's default branch)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *tenant == "" {
		return fmt.Errorf("seed-file: --tenant is required")
	}
	if *repo == "" {
		return fmt.Errorf("seed-file: --repo is required")
	}
	if *path == "" {
		return fmt.Errorf("seed-file: --path is required")
	}

	var contentBytes []byte
	switch {
	case *content != "":
		contentBytes = []byte(*content)
	case *file != "":
		if *file == "-" {
			data, err := io.ReadAll(os.Stdin)
			if err != nil {
				return fmt.Errorf("read stdin: %w", err)
			}
			contentBytes = data
		} else {
			data, err := os.ReadFile(*file)
			if err != nil {
				return fmt.Errorf("read file %q: %w", *file, err)
			}
			contentBytes = data
		}
	default:
		return fmt.Errorf("seed-file: provide --content or --file")
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
	if !fj.Enabled() {
		return fmt.Errorf("forgejo is not configured (set FORGEJO_BASE_URL and FORGEJO_ADMIN_TOKEN)")
	}

	url, err := seedFile(ctx, newStoreAdapter(st), fj, cfg.Forgejo.PublicURL, seedFileInput{
		TenantRef: *tenant,
		Repo:      *repo,
		Path:      *path,
		Content:   contentBytes,
		Branch:    *branch,
	})
	if err != nil {
		return err
	}
	fmt.Println(url)
	return nil
}
