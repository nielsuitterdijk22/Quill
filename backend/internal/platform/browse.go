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
// contents. Each call authorizes project membership first, then delegates the git
// read to Forgejo using the repository's stored owner/name linkage.

// GetRepo returns a repository's metadata for an authorized actor.
func (s *Service) GetRepo(ctx context.Context, actor Actor, projectSlug, repoSlug string) (db.Repository, error) {
	repo, _, _, err := s.resolveRepo(ctx, actor, projectSlug, repoSlug, false)
	return repo, err
}

// ListBranches returns the git branches of a repository.
func (s *Service) ListBranches(ctx context.Context, actor Actor, projectSlug, repoSlug string) (db.Repository, []forgejo.Branch, error) {
	repo, owner, name, err := s.resolveRepo(ctx, actor, projectSlug, repoSlug, true)
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
func (s *Service) ListCommits(ctx context.Context, actor Actor, projectSlug, repoSlug, ref, path string, limit int) (db.Repository, []forgejo.Commit, error) {
	repo, owner, name, err := s.resolveRepo(ctx, actor, projectSlug, repoSlug, true)
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

// GetCommit returns a single commit's metadata together with its diff parsed
// into per-file hunks — what the commit detail page needs in one call.
func (s *Service) GetCommit(ctx context.Context, actor Actor, projectSlug, repoSlug, sha string) (db.Repository, forgejo.Commit, []forgejo.DiffFile, error) {
	repo, owner, name, err := s.resolveRepo(ctx, actor, projectSlug, repoSlug, true)
	if err != nil {
		return db.Repository{}, forgejo.Commit{}, nil, err
	}
	commit, err := s.forgejo.GetCommit(ctx, owner, name, sha)
	if err != nil {
		return db.Repository{}, forgejo.Commit{}, nil, translateForgejoRead(err)
	}
	diff, err := s.forgejo.GetCommitDiff(ctx, owner, name, sha)
	if err != nil {
		return db.Repository{}, forgejo.Commit{}, nil, translateForgejoRead(err)
	}
	return repo, commit, forgejo.ParseUnifiedDiff(diff), nil
}

// GetContents returns a directory listing or single file at path/ref within a
// repository. An empty ref resolves to the repository's default branch.
func (s *Service) GetContents(ctx context.Context, actor Actor, projectSlug, repoSlug, path, ref string) (db.Repository, *forgejo.Contents, error) {
	repo, owner, name, err := s.resolveRepo(ctx, actor, projectSlug, repoSlug, true)
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

// RenderMarkdown renders markdown text to sanitized HTML in the context of a
// repository, for an authorized actor. It is used to render READMEs the way
// Forgejo would (repo-relative links, references), returning safe HTML.
func (s *Service) RenderMarkdown(ctx context.Context, actor Actor, projectSlug, repoSlug, text string) (string, error) {
	_, owner, name, err := s.resolveRepo(ctx, actor, projectSlug, repoSlug, true)
	if err != nil {
		return "", err
	}
	html, err := s.forgejo.RenderMarkup(ctx, text, owner+"/"+name)
	if err != nil {
		return "", translateForgejoRead(err)
	}
	return html, nil
}

// resolveRepo loads a repository by project+repo slug, authorizes the actor as an project
// member, and (when requireGit is set) resolves the Forgejo owner/name needed for
// git-side reads. It returns ErrNotFound, ErrForbidden, or ErrUnavailable as
// appropriate.
func (s *Service) resolveRepo(ctx context.Context, actor Actor, projectSlug, repoSlug string, requireGit bool) (db.Repository, string, string, error) {
	project, err := s.getProject(ctx, actor, projectSlug)
	if err != nil {
		return db.Repository{}, "", "", err
	}
	if err := s.authorizeProjectMember(ctx, actor, project.ID); err != nil {
		return db.Repository{}, "", "", err
	}
	repo, err := s.store.GetRepositoryBySlug(ctx, db.GetRepositoryBySlugParams{
		ProjectID: project.ID,
		Lower:     normalizeSlug(repoSlug),
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
	owner, name, ok := forgejoTarget(repo, project)
	if !ok {
		return db.Repository{}, "", "", ErrUnavailable
	}
	return repo, owner, name, nil
}

// ResolveRepoCoords resolves a project/repo slug pair to the Forgejo owner and
// repo name. Returns ErrNotFound, ErrForbidden, or ErrUnavailable on failure.
func (s *Service) ResolveRepoCoords(ctx context.Context, actor Actor, projectSlug, repoSlug string) (owner, name string, err error) {
	_, o, n, e := s.resolveRepo(ctx, actor, projectSlug, repoSlug, true)
	return o, n, e
}

// forgejoTarget computes the Forgejo (owner, name) pair for a repository, falling
// back to slugs when the explicit linkage columns are unset. ok is false when no
// usable target can be derived.
func forgejoTarget(repo db.Repository, project db.Project) (owner, name string, ok bool) {
	switch {
	case repo.ForgejoOwner.Valid && repo.ForgejoOwner.String != "":
		owner = repo.ForgejoOwner.String
	case project.ForgejoOrgName.Valid && project.ForgejoOrgName.String != "":
		owner = project.ForgejoOrgName.String
	default:
		owner = project.Slug
	}
	if repo.ForgejoName.Valid && repo.ForgejoName.String != "" {
		name = repo.ForgejoName.String
	} else {
		name = repo.Slug
	}
	return owner, name, owner != "" && name != ""
}

// translateForgejoRead maps a Forgejo read error to a platform sentinel: 404s
// become ErrNotFound; 409s signal an empty repository; network-level failures
// (connection refused, DNS, timeout) become ErrUnavailable so callers return 502.
func translateForgejoRead(err error) error {
	if forgejo.NotFound(err) {
		return ErrNotFound
	}
	if forgejo.StatusCode(err) == 409 {
		return ErrEmptyRepo
	}
	if forgejo.IsNetworkError(err) {
		return fmt.Errorf("%w: %s", ErrUnavailable, err)
	}
	return err
}
