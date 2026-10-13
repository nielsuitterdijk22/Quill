package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/nielsuitterdijk22/quill/internal/forgejo"
	"github.com/nielsuitterdijk22/quill/internal/store/db"
)

// seedForgejoLike is the slice of the Forgejo client seedFile needs. It is an
// interface (rather than the concrete *forgejo.Client) so the unit test can
// substitute an in-memory double; the production path passes the real client,
// which satisfies it.
type seedForgejoLike interface {
	Enabled() bool
	GetRepo(ctx context.Context, owner, name string) (forgejo.Repo, error)
	GetOrCreateOrg(ctx context.Context, opts forgejo.CreateOrgOptions) (forgejo.Org, error)
	CreateOrgRepo(ctx context.Context, org string, opts forgejo.CreateRepoOptions) (forgejo.Repo, error)
	SeedFile(ctx context.Context, owner, repo, path, message, branch string, content []byte) (string, error)
}

// seedFileInput is the payload for seedFile.
type seedFileInput struct {
	TenantRef string
	Repo      string
	Path      string
	Content   []byte
	Branch    string
}

// seedFile resolves the tenant and repository, then creates or updates the file
// at path in the repository's default branch (or in.Branch) via the Forgejo
// contents API, committing with a message that marks it as an admin seed. It
// returns the repository URL.
func seedFile(ctx context.Context, st storeLike, fj seedForgejoLike, publicURL string, in seedFileInput) (string, error) {
	tenantID, err := resolveTenantRef(ctx, st, in.TenantRef)
	if err != nil {
		return "", err
	}
	slug, err := normalizeRepoName(in.Repo)
	if err != nil {
		return "", fmt.Errorf("invalid repository %q: %w", in.Repo, err)
	}
	if strings.TrimSpace(in.Path) == "" {
		return "", fmt.Errorf("seed-file: --path is required")
	}

	tenant, err := st.GetTenantByID(ctx, tenantID)
	if err != nil {
		return "", fmt.Errorf("get tenant: %w", err)
	}

	// Resolve the Forgejo org that owns the tenant's repositories: the
	// forgejo_org_name of the tenant's first project, else the tenant slug.
	projects, err := st.ListProjectsByTenant(ctx, tenantID)
	if err != nil {
		return "", fmt.Errorf("list tenant projects: %w", err)
	}
	var project *db.Project
	if len(projects) > 0 {
		project = &projects[0]
	}
	owner := tenant.Slug
	if project != nil && project.ForgejoOrgName.Valid && project.ForgejoOrgName.String != "" {
		owner = project.ForgejoOrgName.String
	}

	if !fj.Enabled() {
		return "", fmt.Errorf("forgejo is not configured (set FORGEJO_BASE_URL and FORGEJO_ADMIN_TOKEN)")
	}

	// The repository must exist: fetch it, creating it (and the org) if not.
	repo, err := fj.GetRepo(ctx, owner, slug)
	if err != nil {
		if !forgejo.NotFound(err) {
			return "", fmt.Errorf("get repository: %w", err)
		}
		if _, oerr := fj.GetOrCreateOrg(ctx, forgejo.CreateOrgOptions{
			Name:       owner,
			FullName:   tenant.Name,
			Visibility: "private",
		}); oerr != nil {
			return "", fmt.Errorf("ensure forgejo org %q: %w", owner, oerr)
		}
		repo, err = fj.CreateOrgRepo(ctx, owner, forgejo.CreateRepoOptions{
			Name:          slug,
			AutoInit:      true,
			DefaultBranch: "main",
		})
		if err != nil {
			return "", fmt.Errorf("create forgejo repo: %w", err)
		}
	}

	// Self-heal the Postgres side too: if the repository was never registered
	// (no row in the repositories table) persist it now, the same way
	// createRepo does. recordRepo is idempotent — an existing row is updated
	// in place.
	if err := recordRepo(ctx, st, st, project, tenant, owner, slug, slug, "", "private", repo); err != nil {
		return "", err
	}

	branch := in.Branch
	if branch == "" {
		branch = repo.DefaultBranch
	}
	message := fmt.Sprintf("Seed %s (admin seed-file)", strings.Trim(in.Path, "/"))
	if _, err := fj.SeedFile(ctx, owner, slug, strings.Trim(in.Path, "/"), message, branch, in.Content); err != nil {
		return "", err
	}
	return repoURL(publicURL, owner, repo), nil
}
