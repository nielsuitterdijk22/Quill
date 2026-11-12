package platform

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/google/uuid"
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
	repo, _, err := s.resolveBrowseRepo(ctx, actor, projectSlug, repoSlug, false)
	return repo, err
}

// ListBranches returns the git branches of a repository (engine-backed for
// own-engine repos, Forgejo otherwise).
func (s *Service) ListBranches(ctx context.Context, actor Actor, projectSlug, repoSlug string) (db.Repository, []forgejo.Branch, error) {
	repo, be, err := s.resolveBrowseRepo(ctx, actor, projectSlug, repoSlug, true)
	if err != nil {
		return db.Repository{}, nil, err
	}
	if be.engine {
		dir, err := s.engineDir(ctx, be)
		if err != nil {
			return db.Repository{}, nil, err
		}
		branches, err := bareListBranches(dir)
		if err != nil {
			return db.Repository{}, nil, err
		}
		return repo, branches, nil
	}
	branches, err := s.forgejo.ListBranches(ctx, be.owner, be.name)
	if err != nil {
		return db.Repository{}, nil, translateForgejoRead(err)
	}
	return repo, branches, nil
}

// ListCommits returns the commit log of a repository at ref, optionally filtered
// to commits touching path.
func (s *Service) ListCommits(ctx context.Context, actor Actor, projectSlug, repoSlug, ref, path string, limit int) (db.Repository, []forgejo.Commit, error) {
	repo, be, err := s.resolveBrowseRepo(ctx, actor, projectSlug, repoSlug, true)
	if err != nil {
		return db.Repository{}, nil, err
	}
	if ref == "" {
		ref = repo.DefaultBranch
	}
	if be.engine {
		empty, err := s.engineRepoEmpty(ctx, be)
		if err != nil {
			return db.Repository{}, nil, err
		}
		if empty {
			return db.Repository{}, nil, ErrEmptyRepo
		}
		dir, err := s.engineDir(ctx, be)
		if err != nil {
			return db.Repository{}, nil, err
		}
		commits, err := bareListCommits(dir, ref, path, limit)
		if err != nil {
			return db.Repository{}, nil, err
		}
		return repo, commits, nil
	}
	commits, err := s.forgejo.ListCommits(ctx, be.owner, be.name, ref, path, limit)
	if err != nil {
		return db.Repository{}, nil, translateForgejoRead(err)
	}
	return repo, commits, nil
}

// GetCommit returns a single commit's metadata together with its diff parsed
// into per-file hunks — what the commit detail page needs in one call.
func (s *Service) GetCommit(ctx context.Context, actor Actor, projectSlug, repoSlug, sha string) (db.Repository, forgejo.Commit, []forgejo.DiffFile, error) {
	repo, be, err := s.resolveBrowseRepo(ctx, actor, projectSlug, repoSlug, true)
	if err != nil {
		return db.Repository{}, forgejo.Commit{}, nil, err
	}
	if be.engine {
		dir, err := s.engineDir(ctx, be)
		if err != nil {
			return db.Repository{}, forgejo.Commit{}, nil, err
		}
		commit, err := bareGetCommit(dir, sha)
		if err != nil {
			return db.Repository{}, forgejo.Commit{}, nil, err
		}
		diff, err := bareCommitDiff(dir, sha)
		if err != nil {
			return db.Repository{}, forgejo.Commit{}, nil, err
		}
		return repo, commit, forgejo.ParseUnifiedDiff(diff), nil
	}
	commit, err := s.forgejo.GetCommit(ctx, be.owner, be.name, sha)
	if err != nil {
		return db.Repository{}, forgejo.Commit{}, nil, translateForgejoRead(err)
	}
	diff, err := s.forgejo.GetCommitDiff(ctx, be.owner, be.name, sha)
	if err != nil {
		return db.Repository{}, forgejo.Commit{}, nil, translateForgejoRead(err)
	}
	return repo, commit, forgejo.ParseUnifiedDiff(diff), nil
}

// GetContents returns a directory listing or single file at path/ref within a
// repository. An empty ref resolves to the repository's default branch.
func (s *Service) GetContents(ctx context.Context, actor Actor, projectSlug, repoSlug, path, ref string) (db.Repository, *forgejo.Contents, error) {
	repo, be, err := s.resolveBrowseRepo(ctx, actor, projectSlug, repoSlug, true)
	if err != nil {
		return db.Repository{}, nil, err
	}
	if ref == "" {
		ref = repo.DefaultBranch
	}
	if be.engine {
		empty, err := s.engineRepoEmpty(ctx, be)
		if err != nil {
			return db.Repository{}, nil, err
		}
		if empty {
			return db.Repository{}, nil, ErrEmptyRepo
		}
		dir, err := s.engineDir(ctx, be)
		if err != nil {
			return db.Repository{}, nil, err
		}
		contents, err := bareGetContents(dir, path, ref)
		if err != nil {
			return db.Repository{}, nil, err
		}
		if contents.IsDir {
			s.attachLastCommits(ctx, be, ref, contents.Entries)
		}
		return repo, contents, nil
	}
	contents, err := s.forgejo.GetContents(ctx, be.owner, be.name, path, ref)
	if err != nil {
		return db.Repository{}, nil, translateForgejoRead(err)
	}
	if contents.IsDir {
		s.attachLastCommits(ctx, be, ref, contents.Entries)
	}
	return repo, contents, nil
}

// lastCommitWorkers bounds how many per-entry commit lookups run at once so a
// large directory does not fan out into an unbounded burst of git processes
// (engine) or Forgejo requests (forgejo).
const lastCommitWorkers = 8

// attachLastCommits fills in the LastCommit of each directory entry by fetching
// the most recent commit touching its path. Lookups run concurrently with a
// bounded worker pool. It is best-effort: an entry whose lookup fails simply
// keeps a nil LastCommit rather than failing the whole listing.
func (s *Service) attachLastCommits(ctx context.Context, be gitBackend, ref string, entries []forgejo.ContentEntry) {
	if len(entries) == 0 {
		return
	}
	// Engine-backed: one git log per entry against the hydrated dir.
	if be.engine {
		dir, err := s.engineDir(ctx, be)
		if err != nil {
			return
		}
		sem := make(chan struct{}, lastCommitWorkers)
		var wg sync.WaitGroup
		for i := range entries {
			wg.Add(1)
			sem <- struct{}{}
			go func(i int) {
				defer wg.Done()
				defer func() { <-sem }()
				ec, err := bareLastCommit(dir, ref, entries[i].Path)
				if err == nil {
					entries[i].LastCommit = ec
				}
			}(i)
		}
		wg.Wait()
		return
	}
	sem := make(chan struct{}, lastCommitWorkers)
	var wg sync.WaitGroup
	for i := range entries {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int) {
			defer wg.Done()
			defer func() { <-sem }()
			commits, err := s.forgejo.ListCommits(ctx, be.owner, be.name, ref, entries[i].Path, 1)
			if err != nil || len(commits) == 0 {
				return
			}
			c := commits[0]
			ec := &forgejo.EntryCommit{
				SHA:        c.SHA,
				Message:    c.Commit.Message,
				AuthorName: c.Commit.Author.Name,
				Date:       c.Commit.Author.Date,
			}
			if c.Author != nil {
				ec.AuthorLogin = c.Author.Login
			}
			entries[i].LastCommit = ec
		}(i)
	}
	wg.Wait()
}

// RenderMarkdown renders markdown text to sanitized HTML in the context of a
// repository, for an authorized actor. Engine-backed repos render GFM
// locally (goldmark); Forgejo repos keep the Forgejo renderer until Phase D.
func (s *Service) RenderMarkdown(ctx context.Context, actor Actor, projectSlug, repoSlug, text string) (string, error) {
	_, be, err := s.resolveBrowseRepo(ctx, actor, projectSlug, repoSlug, true)
	if err != nil {
		return "", err
	}
	if be.engine {
		return renderGFM(text), nil
	}
	html, err := s.forgejo.RenderMarkup(ctx, text, be.owner+"/"+be.name)
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
	return s.resolveRepoChecked(ctx, actor, projectSlug, repoSlug, requireGit, false)
}

// resolveRepoForRead's public-read bypass semantics moved into resolveBrowseRepo
// (engine_browse.go, PR B4); the member-only resolveRepo/resolveRepoChecked
// variants below serve the write-scope callsites unchanged.

// resolveRepoChecked implements both variants: member authorization, relaxed
// by the public-read bypass when allowPublicRead is set.
func (s *Service) resolveRepoChecked(ctx context.Context, actor Actor, projectSlug, repoSlug string, requireGit, allowPublicRead bool) (db.Repository, string, string, error) {
	project, err := s.getProject(ctx, projectSlug)
	if err != nil {
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
	if err := s.authorizeProjectMember(ctx, actor, project.ID); err != nil {
		if errors.Is(err, ErrForbidden) && allowPublicRead && repo.Visibility == VisibilityPublic {
			// Public repositories are world-readable: the visibility on the
			// repo row is the authorization for read-only browsing.
			if actor.UserID == uuid.Nil {
				s.logger.Debug("anonymous public repo read", "project", projectSlug, "repo", repoSlug)
			}
		} else if errors.Is(err, ErrForbidden) && allowPublicRead && actor.UserID == uuid.Nil {
			// Anonymous callers must not learn that a non-public repo exists.
			return db.Repository{}, "", "", ErrNotFound
		} else {
			return db.Repository{}, "", "", err
		}
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
