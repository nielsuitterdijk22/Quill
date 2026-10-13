package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nielsuitterdijk22/quill/internal/forgejo"
	"github.com/nielsuitterdijk22/quill/internal/store/db"
)

// dbQuerier is the slice of the generated db.Queries the CLI touches. The
// production adapter (storeAdapter) and the in-memory test double both satisfy
// it, so createRepo is testable without a live Postgres.
type dbQuerier interface {
	GetTenantByID(ctx context.Context, id uuid.UUID) (db.Tenant, error)
	GetTenantBySlug(ctx context.Context, lower string) (db.Tenant, error)
	GetRepositoryBySlug(ctx context.Context, arg db.GetRepositoryBySlugParams) (db.Repository, error)
	CreateRepository(ctx context.Context, arg db.CreateRepositoryParams) (db.Repository, error)
	UpdateRepository(ctx context.Context, arg db.UpdateRepositoryParams) (db.Repository, error)
	SetRepositoryForgejoLink(ctx context.Context, arg db.SetRepositoryForgejoLinkParams) (db.Repository, error)
}

// storeLike is the store surface createRepo needs beyond the generated queries:
// transactional writes and the hand-written tenant/project lookups. The InTx
// callback receives a dbQuerier (the transaction-scoped *db.Queries in
// production, the store itself in the in-memory test double) so the repository
// writes run in the same transaction as the surrounding work.
type storeLike interface {
	dbQuerier
	InTx(ctx context.Context, fn func(q dbQuerier) error) error
	ListProjectsByTenant(ctx context.Context, tenantID uuid.UUID) ([]db.Project, error)
	CreateProject(ctx context.Context, arg db.CreateProjectParams) (db.Project, error)
}

// createRepoInput is the payload for createRepo.
type createRepoInput struct {
	TenantRef   string
	Name        string
	Description string
	Visibility  string
}

// createRepo resolves the tenant's Forgejo org, creates the repository there,
// records it in Postgres, and returns the repository URL. It is idempotent:
// re-running it for an existing repository returns the existing URL.
func createRepo(ctx context.Context, st storeLike, fj *forgejo.Client, publicURL string, in createRepoInput) (string, error) {
	tenantID, err := resolveTenantRef(ctx, st, in.TenantRef)
	if err != nil {
		return "", err
	}
	slug, err := normalizeRepoName(in.Name)
	if err != nil {
		return "", err
	}
	if !validVisibility(in.Visibility) {
		return "", fmt.Errorf("invalid visibility %q (want public, internal or private)", in.Visibility)
	}

	tenant, err := st.GetTenantByID(ctx, tenantID)
	if err != nil {
		return "", fmt.Errorf("get tenant: %w", err)
	}

	// Resolve the Forgejo org that owns the tenant's repositories: the
	// forgejo_org_name of the tenant's first project, else the tenant slug.
	// GetOrCreateOrg self-heals a missing org on demand.
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
	if _, err := fj.GetOrCreateOrg(ctx, forgejo.CreateOrgOptions{
		Name:       owner,
		FullName:   tenant.Name,
		Visibility: "private",
	}); err != nil {
		return "", fmt.Errorf("ensure forgejo org %q: %w", owner, err)
	}

	repo, err := fj.CreateOrgRepo(ctx, owner, forgejo.CreateRepoOptions{
		Name:          slug,
		Description:   in.Description,
		Private:       in.Visibility != "public",
		AutoInit:      true,
		DefaultBranch: "main",
	})
	if err != nil {
		if isAlreadyExists(err) {
			repo, err = fj.GetRepo(ctx, owner, slug)
			if err != nil {
				return "", fmt.Errorf("repository already exists but could not be fetched: %w", err)
			}
		} else {
			return "", fmt.Errorf("create forgejo repo: %w", err)
		}
	}

	if err := recordRepo(ctx, st, st, project, tenant, owner, slug, in.Name, in.Description, in.Visibility, repo); err != nil {
		return "", err
	}
	return repoURL(publicURL, owner, repo), nil
}

// resolveTenantRef accepts a tenant UUID or slug and returns the tenant ID.
func resolveTenantRef(ctx context.Context, st storeLike, ref string) (uuid.UUID, error) {
	if id, err := uuid.Parse(ref); err == nil {
		return id, nil
	}
	tenant, err := st.GetTenantBySlug(ctx, ref)
	if err != nil {
		return uuid.UUID{}, fmt.Errorf("resolve tenant %q: not a UUID and no tenant with that slug", ref)
	}
	return tenant.ID, nil
}

// recordRepo writes the repository into Postgres. If the project already holds a
// row for this slug it is updated in place (idempotent re-runs); otherwise a new
// row is created. When the tenant has no project yet one is created first so the
// repository has a home. All writes happen in one transaction on q.
func recordRepo(ctx context.Context, st storeLike, q dbQuerier, project *db.Project, tenant db.Tenant, owner, slug, name, description, visibility string, repo forgejo.Repo) error {
	if project == nil {
		p, err := st.CreateProject(ctx, db.CreateProjectParams{
			TenantID: tenant.ID,
			Slug:     tenant.Slug,
			Name:     tenant.Name,
		})
		if err != nil {
			return fmt.Errorf("create project for tenant: %w", err)
		}
		project = &p
	}

	err := st.InTx(ctx, func(qq dbQuerier) error {
		q = qq
		existing, err := q.GetRepositoryBySlug(ctx, db.GetRepositoryBySlugParams{ProjectID: project.ID, Lower: slug})
		switch {
		case err == nil:
			if _, uerr := q.UpdateRepository(ctx, db.UpdateRepositoryParams{
				ID:            existing.ID,
				Slug:          existing.Slug,
				Name:          name,
				Description:   description,
				Visibility:    visibility,
				DefaultBranch: existing.DefaultBranch,
				IsArchived:    existing.IsArchived,
				ForgejoName:   pgText(repo.Name),
			}); uerr != nil {
				return fmt.Errorf("update existing repository row: %w", uerr)
			}
			_, lerr := q.SetRepositoryForgejoLink(ctx, db.SetRepositoryForgejoLinkParams{
				ID:            existing.ID,
				ForgejoRepoID: pgInt8(repo.ID),
				ForgejoOwner:  pgText(owner),
				ForgejoName:   pgText(repo.Name),
			})
			return lerr
		case isNoRows(err):
			created, cerr := q.CreateRepository(ctx, db.CreateRepositoryParams{
				ProjectID:     project.ID,
				Slug:          slug,
				Name:          name,
				Description:   description,
				Visibility:    visibility,
				DefaultBranch: "main",
			})
			if cerr != nil {
				return fmt.Errorf("create repository row: %w", cerr)
			}
			_, lerr := q.SetRepositoryForgejoLink(ctx, db.SetRepositoryForgejoLinkParams{
				ID:            created.ID,
				ForgejoRepoID: pgInt8(repo.ID),
				ForgejoOwner:  pgText(owner),
				ForgejoName:   pgText(repo.Name),
			})
			return lerr
		default:
			return fmt.Errorf("lookup repository: %w", err)
		}
	})
	if err != nil {
		return fmt.Errorf("record repository: %w", err)
	}
	return nil
}

// repoURL returns the repository's human-facing URL, preferring Forgejo's
// html_url and falling back to the configured public/base URL.
func repoURL(publicURL, owner string, repo forgejo.Repo) string {
	if repo.HTMLURL != "" {
		return repo.HTMLURL
	}
	base := strings.TrimSuffix(publicURL, "/")
	if base == "" {
		return ""
	}
	return fmt.Sprintf("%s/%s/%s", base, owner, repo.Name)
}

// isNoRows reports whether err is the pgx no-rows sentinel.
func isNoRows(err error) bool {
	return errors.Is(err, pgx.ErrNoRows)
}

// isAlreadyExists reports whether err is a Forgejo "already exists" conflict.
func isAlreadyExists(err error) bool {
	if err == nil {
		return false
	}
	if forgejo.StatusCode(err) == 409 {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "already exists") || strings.Contains(msg, "already exist")
}

// pgInt8 builds a valid pgtype.Int8.
func pgInt8(v int64) pgtype.Int8 {
	return pgtype.Int8{Int64: v, Valid: true}
}

// pgText builds a valid pgtype.Text.
func pgText(v string) pgtype.Text {
	return pgtype.Text{String: v, Valid: true}
}
