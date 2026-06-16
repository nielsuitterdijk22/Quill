package platform

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

// CreateRepoInput is the payload for creating a repository under a project.
type CreateRepoInput struct {
	Slug        string
	Name        string
	Description string
	Visibility  string // public | internal | private (default private)
}

// CreateRepo provisions a repository under projectSlug for an authorized actor.
// With Forgejo enabled it creates the git repository (auto-initialised) in
// Forgejo first, then records the repo and its Forgejo linkage in Postgres; the
// Forgejo repo is deleted if the local write fails.
func (s *Service) CreateRepo(ctx context.Context, actor Actor, projectSlug string, in CreateRepoInput) (db.Repository, error) {
	project, err := s.getProject(ctx, projectSlug)
	if err != nil {
		return db.Repository{}, err
	}
	if err := s.authorizeProjectMember(ctx, actor, project.ID); err != nil {
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

	// Fail fast on a known-taken slug to avoid orphaning a Forgejo repo.
	if _, err := s.store.GetRepositoryBySlug(ctx, db.GetRepositoryBySlugParams{ProjectID: project.ID, Lower: slug}); err == nil {
		return db.Repository{}, ErrConflict
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return db.Repository{}, fmt.Errorf("lookup repo: %w", err)
	}

	const defaultBranch = "main"

	owner := project.Slug
	if project.ForgejoOrgName.Valid && project.ForgejoOrgName.String != "" {
		owner = project.ForgejoOrgName.String
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
			ProjectID:     project.ID,
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

// ListReposByProject returns repositories in a project ordered by slug, for an
// authorized actor.
func (s *Service) ListReposByProject(ctx context.Context, actor Actor, projectSlug string, limit, offset int32) (db.Project, []db.Repository, error) {
	project, err := s.getProject(ctx, projectSlug)
	if err != nil {
		return db.Project{}, nil, err
	}
	if err := s.authorizeProjectMember(ctx, actor, project.ID); err != nil {
		return db.Project{}, nil, err
	}
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	repos, err := s.store.ListRepositoriesByProject(ctx, db.ListRepositoriesByProjectParams{
		ProjectID: project.ID,
		Limit:     limit,
		Offset:    offset,
	})
	return project, repos, err
}

// UpdateRepoInput is a partial update of a repository's settings. A nil field is
// left unchanged; a non-nil field is applied (and validated). Setting Slug
// renames the repository, which also renames its Forgejo git repository.
type UpdateRepoInput struct {
	Name          *string
	Description   *string
	Visibility    *string
	DefaultBranch *string
	Slug          *string
	Archived      *bool
}

// UpdateRepo changes a repository's general settings for a project owner or
// platform admin. The change is mirrored into Forgejo first — so a rename or
// default-branch move is validated by the git layer — and then recorded in
// Postgres. A rename that fails to persist is rolled back in Forgejo so the two
// systems don't drift.
func (s *Service) UpdateRepo(ctx context.Context, actor Actor, projectSlug, repoSlug string, in UpdateRepoInput) (db.Repository, error) {
	project, err := s.getProject(ctx, projectSlug)
	if err != nil {
		return db.Repository{}, err
	}
	if err := s.authorizeProjectAdmin(ctx, actor, project.ID); err != nil {
		return db.Repository{}, err
	}
	repo, err := s.store.GetRepositoryBySlug(ctx, db.GetRepositoryBySlugParams{
		ProjectID: project.ID,
		Lower:     normalizeSlug(repoSlug),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return db.Repository{}, ErrNotFound
	}
	if err != nil {
		return db.Repository{}, fmt.Errorf("lookup repo: %w", err)
	}

	// Apply the requested fields onto a copy of the current state, validating each.
	next := repo
	renamed := false

	if in.Name != nil {
		name := strings.TrimSpace(*in.Name)
		if name == "" {
			return db.Repository{}, fmt.Errorf("%w: name cannot be empty", ErrInvalidInput)
		}
		next.Name = name
	}
	if in.Description != nil {
		next.Description = strings.TrimSpace(*in.Description)
	}
	if in.Visibility != nil {
		vis := strings.TrimSpace(*in.Visibility)
		if !validVisibility(vis) {
			return db.Repository{}, fmt.Errorf("%w: visibility must be public, internal or private", ErrInvalidInput)
		}
		next.Visibility = vis
	}
	if in.DefaultBranch != nil {
		branch := strings.TrimSpace(*in.DefaultBranch)
		if branch == "" {
			return db.Repository{}, fmt.Errorf("%w: default branch cannot be empty", ErrInvalidInput)
		}
		next.DefaultBranch = branch
	}
	if in.Archived != nil {
		next.IsArchived = *in.Archived
	}
	if in.Slug != nil {
		slug := normalizeSlug(*in.Slug)
		if slug != repo.Slug {
			if !validSlug(slug) {
				return db.Repository{}, fmt.Errorf("%w: slug must be 1-63 chars of lowercase letters, digits, '-', '_' or '.', start alphanumeric, and not be a reserved word", ErrInvalidInput)
			}
			// Reject a slug already taken by another repo in the same project.
			if _, err := s.store.GetRepositoryBySlug(ctx, db.GetRepositoryBySlugParams{ProjectID: project.ID, Lower: slug}); err == nil {
				return db.Repository{}, ErrConflict
			} else if !errors.Is(err, pgx.ErrNoRows) {
				return db.Repository{}, fmt.Errorf("lookup repo: %w", err)
			}
			next.Slug = slug
			renamed = true
		}
	}

	if !repoChanged(repo, next) {
		return repo, nil // nothing to do
	}

	// Mirror the git-visible changes into Forgejo. The display name is Quill-only
	// metadata (Forgejo has no separate display name), so it is not mirrored.
	if s.forgejoEnabled() {
		owner, name, ok := forgejoTarget(repo, project)
		if !ok {
			return db.Repository{}, ErrUnavailable
		}
		opts, dirty := forgejoEdit(repo, next)
		// currentName tracks where the repo lives in Forgejo; it changes after a
		// rename so any compensating revert targets the right repository.
		currentName := name
		if dirty {
			updated, err := s.forgejo.EditRepo(ctx, owner, name, opts)
			if err != nil {
				return db.Repository{}, translateForgejoWrite(err)
			}
			if renamed {
				next.ForgejoName = pgtype.Text{String: updated.Name, Valid: true}
				currentName = updated.Name
			}
		}

		saved, err := s.store.UpdateRepository(ctx, updateRepoParams(repo.ID, next))
		if err != nil {
			// The Forgejo edit already applied but persisting failed. Revert the
			// *entire* edit — not just a rename — so the two systems don't silently
			// diverge. This matters most for visibility: a private→public change
			// left in Forgejo would expose a repo Quill still treats as private.
			if dirty {
				cctx, cancel := detachedContext(ctx)
				defer cancel()
				if revert, rdirty := forgejoEdit(next, repo); rdirty {
					if _, rerr := s.forgejo.EditRepo(cctx, owner, currentName, revert); rerr != nil {
						s.logger.Error("failed to roll back forgejo repo edit", "repo", owner+"/"+currentName, "error", rerr)
					}
				}
			}
			if isUniqueViolation(err) {
				return db.Repository{}, ErrConflict
			}
			return db.Repository{}, fmt.Errorf("update repo: %w", err)
		}
		return saved, nil
	}

	saved, err := s.store.UpdateRepository(ctx, updateRepoParams(repo.ID, next))
	if err != nil {
		if isUniqueViolation(err) {
			return db.Repository{}, ErrConflict
		}
		return db.Repository{}, fmt.Errorf("update repo: %w", err)
	}
	return saved, nil
}

// DeleteRepo permanently removes a repository for a project owner or platform
// admin. The git repository is deleted from Forgejo first — an already-missing
// one is treated as success — and then the metadata row (cascading its branch
// policies) is removed, which keeps a retried delete safe.
func (s *Service) DeleteRepo(ctx context.Context, actor Actor, projectSlug, repoSlug string) error {
	project, err := s.getProject(ctx, projectSlug)
	if err != nil {
		return err
	}
	if err := s.authorizeProjectAdmin(ctx, actor, project.ID); err != nil {
		return err
	}
	repo, err := s.store.GetRepositoryBySlug(ctx, db.GetRepositoryBySlugParams{
		ProjectID: project.ID,
		Lower:     normalizeSlug(repoSlug),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("lookup repo: %w", err)
	}

	if s.forgejoEnabled() {
		if owner, name, ok := forgejoTarget(repo, project); ok {
			if err := s.forgejo.DeleteRepo(ctx, owner, name); err != nil && !forgejo.NotFound(err) {
				return fmt.Errorf("forgejo delete repo: %w", err)
			}
		}
	}
	if err := s.store.DeleteRepository(ctx, repo.ID); err != nil {
		return fmt.Errorf("delete repo: %w", err)
	}
	return nil
}

// repoChanged reports whether any persisted setting differs between two repos.
func repoChanged(a, b db.Repository) bool {
	return a.Slug != b.Slug ||
		a.Name != b.Name ||
		a.Description != b.Description ||
		a.Visibility != b.Visibility ||
		a.DefaultBranch != b.DefaultBranch ||
		a.IsArchived != b.IsArchived
}

// forgejoEdit builds the Forgejo edit payload from the diff between the current
// and desired repository, returning dirty=false when nothing git-visible changed.
func forgejoEdit(cur, next db.Repository) (forgejo.EditRepoOptions, bool) {
	var opts forgejo.EditRepoOptions
	dirty := false
	if next.Slug != cur.Slug {
		s := next.Slug
		opts.Name = &s
		dirty = true
	}
	if next.Description != cur.Description {
		d := next.Description
		opts.Description = &d
		dirty = true
	}
	if next.Visibility != cur.Visibility {
		p := forgejoPrivate(next.Visibility)
		opts.Private = &p
		dirty = true
	}
	if next.DefaultBranch != cur.DefaultBranch {
		b := next.DefaultBranch
		opts.DefaultBranch = &b
		dirty = true
	}
	if next.IsArchived != cur.IsArchived {
		a := next.IsArchived
		opts.Archived = &a
		dirty = true
	}
	return opts, dirty
}

// updateRepoParams projects a repository's desired state into UpdateRepository args.
func updateRepoParams(id uuid.UUID, next db.Repository) db.UpdateRepositoryParams {
	return db.UpdateRepositoryParams{
		ID:            id,
		Slug:          next.Slug,
		Name:          next.Name,
		Description:   next.Description,
		Visibility:    next.Visibility,
		DefaultBranch: next.DefaultBranch,
		IsArchived:    next.IsArchived,
		ForgejoName:   next.ForgejoName,
	}
}
