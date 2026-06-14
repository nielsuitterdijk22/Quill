package platform

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nielsuitterdijk22/quill/internal/forgejo"
	"github.com/nielsuitterdijk22/quill/internal/store/db"
)

// CreateRepoInput is the payload for creating a repository under an org.
type CreateRepoInput struct {
	Slug           string
	Name           string
	Description    string
	Visibility     string // public | internal | private (default private)
	OwningTeamSlug string // defaults to the org's "owners" team
}

// CreateRepo provisions a repository under orgSlug for an authorized actor. With
// Forgejo enabled it creates the git repository (auto-initialised) in Forgejo
// first, then records the repo and its Forgejo linkage in Postgres; the Forgejo
// repo is deleted if the local write fails.
func (s *Service) CreateRepo(ctx context.Context, actor Actor, orgSlug string, in CreateRepoInput) (db.Repository, error) {
	org, err := s.getOrg(ctx, orgSlug)
	if err != nil {
		return db.Repository{}, err
	}
	if err := s.authorizeOrgMember(ctx, actor, org.ID); err != nil {
		return db.Repository{}, err
	}

	slug := normalizeSlug(in.Slug)
	name := strings.TrimSpace(in.Name)
	if !validSlug(slug) {
		return db.Repository{}, fmt.Errorf("%w: slug must be 1-63 chars of lowercase letters, digits, '-', '_' or '.', start alphanumeric, and not be a reserved word", ErrInvalidInput)
	}
	if name == "" {
		name = slug
	}
	visibility := in.Visibility
	if visibility == "" {
		visibility = VisibilityPrivate
	}
	if !validVisibility(visibility) {
		return db.Repository{}, fmt.Errorf("%w: visibility must be public, internal or private", ErrInvalidInput)
	}

	teamSlug := normalizeSlug(in.OwningTeamSlug)
	if teamSlug == "" {
		teamSlug = defaultOwningTeamSlug
	}
	team, err := s.store.GetTeamBySlug(ctx, db.GetTeamBySlugParams{OrgID: org.ID, Lower: teamSlug})
	if errors.Is(err, pgx.ErrNoRows) {
		return db.Repository{}, fmt.Errorf("%w: owning team %q does not exist", ErrInvalidInput, teamSlug)
	} else if err != nil {
		return db.Repository{}, fmt.Errorf("lookup owning team: %w", err)
	}

	// Fail fast on a known-taken slug to avoid orphaning a Forgejo repo.
	if _, err := s.store.GetRepositoryBySlug(ctx, db.GetRepositoryBySlugParams{OrgID: org.ID, Lower: slug}); err == nil {
		return db.Repository{}, ErrConflict
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return db.Repository{}, fmt.Errorf("lookup repo: %w", err)
	}

	const defaultBranch = "main"

	owner := org.Slug
	if org.ForgejoOrgName.Valid && org.ForgejoOrgName.String != "" {
		owner = org.ForgejoOrgName.String
	}

	var (
		fjID            pgtype.Int8
		fjOwner, fjName pgtype.Text
		fjCreated       bool
	)
	if s.forgejoEnabled() {
		repo, err := s.forgejo.CreateOrgRepo(ctx, owner, forgejo.CreateRepoOptions{
			Name:          slug,
			Description:   strings.TrimSpace(in.Description),
			Private:       forgejoPrivate(visibility),
			AutoInit:      true,
			DefaultBranch: defaultBranch,
		})
		if err != nil {
			return db.Repository{}, fmt.Errorf("forgejo create repo: %w", err)
		}
		fjID = pgtype.Int8{Int64: repo.ID, Valid: true}
		fjOwner = pgtype.Text{String: owner, Valid: true}
		fjName = pgtype.Text{String: repo.Name, Valid: true}
		fjCreated = true
	}

	var created db.Repository
	err = s.store.InTx(ctx, func(q *db.Queries) error {
		repo, err := q.CreateRepository(ctx, db.CreateRepositoryParams{
			OrgID:         org.ID,
			OwningTeamID:  team.ID,
			Slug:          slug,
			Name:          name,
			Description:   strings.TrimSpace(in.Description),
			Visibility:    visibility,
			DefaultBranch: defaultBranch,
		})
		if err != nil {
			return err
		}
		if fjCreated {
			repo, err = q.SetRepositoryForgejoLink(ctx, db.SetRepositoryForgejoLinkParams{
				ID:            repo.ID,
				ForgejoRepoID: fjID,
				ForgejoOwner:  fjOwner,
				ForgejoName:   fjName,
			})
			if err != nil {
				return err
			}
		}
		created = repo
		return nil
	})
	if err != nil {
		if fjCreated {
			cctx, cancel := detachedContext(ctx)
			defer cancel()
			if delErr := s.forgejo.DeleteRepo(cctx, owner, slug); delErr != nil {
				s.logger.Error("failed to roll back forgejo repo", "owner", owner, "repo", slug, "error", delErr)
			}
		}
		if isUniqueViolation(err) {
			return db.Repository{}, ErrConflict
		}
		return db.Repository{}, err
	}
	return created, nil
}

// ListReposByOrg returns repositories in an org ordered by slug, for an
// authorized actor.
func (s *Service) ListReposByOrg(ctx context.Context, actor Actor, orgSlug string, limit, offset int32) (db.Organization, []db.Repository, error) {
	org, err := s.getOrg(ctx, orgSlug)
	if err != nil {
		return db.Organization{}, nil, err
	}
	if err := s.authorizeOrgMember(ctx, actor, org.ID); err != nil {
		return db.Organization{}, nil, err
	}
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	repos, err := s.store.ListRepositoriesByOrg(ctx, db.ListRepositoriesByOrgParams{
		OrgID:  org.ID,
		Limit:  limit,
		Offset: offset,
	})
	return org, repos, err
}
