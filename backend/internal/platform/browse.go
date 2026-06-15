package platform

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/nielsuitterdijk22/quill/internal/forgejo"
	"github.com/nielsuitterdijk22/quill/internal/store/db"
)

// This file exposes the read-only "browse the code" operations: resolving a
// repository, listing its branches and commits, and reading directory or file
// contents. Each call authorizes org membership first, then delegates the git
// read to Forgejo using the repository's stored owner/name linkage.

// GetRepo returns a repository's metadata for an authorized actor.
func (s *Service) GetRepo(ctx context.Context, actor Actor, orgSlug, repoSlug string) (db.Repository, error) {
	repo, _, _, err := s.resolveRepo(ctx, actor, orgSlug, repoSlug, false)
	return repo, err
}

// ListBranches returns the git branches of a repository.
func (s *Service) ListBranches(ctx context.Context, actor Actor, orgSlug, repoSlug string) (db.Repository, []forgejo.Branch, error) {
	repo, owner, name, err := s.resolveRepo(ctx, actor, orgSlug, repoSlug, true)
	if err != nil {
		return db.Repository{}, nil, err
	}
	branches, err := s.forgejo.ListBranches(ctx, owner, name)
	if err != nil {
		return db.Repository{}, nil, translateForgejoRead(err)
	}
	return repo, branches, nil
}

// ListCommits returns the commit log of a repository at ref, optionally filtered
// to commits touching path.
func (s *Service) ListCommits(ctx context.Context, actor Actor, orgSlug, repoSlug, ref, path string, limit int) (db.Repository, []forgejo.Commit, error) {
	repo, owner, name, err := s.resolveRepo(ctx, actor, orgSlug, repoSlug, true)
	if err != nil {
		return db.Repository{}, nil, err
	}
	if ref == "" {
		ref = repo.DefaultBranch
	}
	commits, err := s.forgejo.ListCommits(ctx, owner, name, ref, path, limit)
	if err != nil {
		return db.Repository{}, nil, translateForgejoRead(err)
	}
	return repo, commits, nil
}

// GetContents returns a directory listing or single file at path/ref within a
// repository. An empty ref resolves to the repository's default branch.
func (s *Service) GetContents(ctx context.Context, actor Actor, orgSlug, repoSlug, path, ref string) (db.Repository, *forgejo.Contents, error) {
	repo, owner, name, err := s.resolveRepo(ctx, actor, orgSlug, repoSlug, true)
	if err != nil {
		return db.Repository{}, nil, err
	}
	if ref == "" {
		ref = repo.DefaultBranch
	}
	contents, err := s.forgejo.GetContents(ctx, owner, name, path, ref)
	if err != nil {
		return db.Repository{}, nil, translateForgejoRead(err)
	}
	return repo, contents, nil
}

// resolveRepo loads a repository by org+repo slug, authorizes the actor as an org
// member, and (when requireGit is set) resolves the Forgejo owner/name needed for
// git-side reads. It returns ErrNotFound, ErrForbidden, or ErrUnavailable as
// appropriate.
func (s *Service) resolveRepo(ctx context.Context, actor Actor, orgSlug, repoSlug string, requireGit bool) (db.Repository, string, string, error) {
	org, err := s.getOrg(ctx, orgSlug)
	if err != nil {
		return db.Repository{}, "", "", err
	}
	if err := s.authorizeOrgMember(ctx, actor, org.ID); err != nil {
		return db.Repository{}, "", "", err
	}
	repo, err := s.store.GetRepositoryBySlug(ctx, db.GetRepositoryBySlugParams{
		OrgID: org.ID,
		Lower: normalizeSlug(repoSlug),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return db.Repository{}, "", "", ErrNotFound
	}
	if err != nil {
		return db.Repository{}, "", "", fmt.Errorf("lookup repo: %w", err)
	}
	if !requireGit {
		return repo, "", "", nil
	}
	if !s.forgejoEnabled() {
		return db.Repository{}, "", "", ErrUnavailable
	}
	owner, name, ok := forgejoTarget(repo, org)
	if !ok {
		return db.Repository{}, "", "", ErrUnavailable
	}
	return repo, owner, name, nil
}

// forgejoTarget computes the Forgejo (owner, name) pair for a repository, falling
// back to slugs when the explicit linkage columns are unset. ok is false when no
// usable target can be derived.
func forgejoTarget(repo db.Repository, org db.Organization) (owner, name string, ok bool) {
	switch {
	case repo.ForgejoOwner.Valid && repo.ForgejoOwner.String != "":
		owner = repo.ForgejoOwner.String
	case org.ForgejoOrgName.Valid && org.ForgejoOrgName.String != "":
		owner = org.ForgejoOrgName.String
	default:
		owner = org.Slug
	}
	if repo.ForgejoName.Valid && repo.ForgejoName.String != "" {
		name = repo.ForgejoName.String
	} else {
		name = repo.Slug
	}
	return owner, name, owner != "" && name != ""
}

// translateForgejoRead maps a Forgejo read error to a platform sentinel: 404s
// become ErrNotFound; 409s signal an empty repository.
func translateForgejoRead(err error) error {
	if forgejo.NotFound(err) {
		return ErrNotFound
	}
	if forgejo.StatusCode(err) == 409 {
		return ErrEmptyRepo
	}
	return err
}
