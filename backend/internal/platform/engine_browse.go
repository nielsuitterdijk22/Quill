package platform

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/nielsuitterdijk22/quill/internal/forgejo"
	"github.com/nielsuitterdijk22/quill/internal/store/db"
)

// Engine-backed code browse (serverless epic, PR B4). The read shapes are the
// forgejo DTOs the API already returns — byte-compatible JSON for the
// frontend — but the data comes from upstream git shelled out in the hydrated
// bare repo (same "upstream git does the work" DNA as the smart-HTTP paths;
// NOT a reimplementation of git plumbing — see DECISIONS.md for why the gitea
// module earns its dependency cost at B5, not here).

// browseDiffBytes caps a single commit diff read (mirrors forgejo.maxDiffBytes).
const browseDiffBytes = 2 << 20

// gitBackend tells one browse call where a repository's content lives.
type gitBackend struct {
	engine bool
	// forgejo target (owner, name) when engine is false.
	owner, name string
	// engine identity when engine is true.
	repoID      uuid.UUID
	tenantID    uuid.UUID
	projectSlug string
	repoSlug    string
}

// resolveBrowseRepo is the B4 resolver: the same auth semantics as the old
// resolveRepoForRead (member OR public-repo bypass; anonymous callers never
// learn about repos they cannot see), plus backend discrimination — a repo
// with a referee row serves from the engine, everything else from Forgejo
// (which also fails closed as ErrUnavailable while Forgejo is disabled).
func (s *Service) resolveBrowseRepo(ctx context.Context, act Actor, projectSlug, repoSlug string, requireGit bool) (db.Repository, gitBackend, error) {
	project, err := s.getProject(ctx, projectSlug)
	if err != nil {
		return db.Repository{}, gitBackend{}, err
	}
	repo, err := s.store.GetRepositoryBySlug(ctx, db.GetRepositoryBySlugParams{ProjectID: project.ID, Lower: normalizeSlug(repoSlug)})
	if errors.Is(err, pgx.ErrNoRows) {
		return db.Repository{}, gitBackend{}, ErrNotFound
	}
	if err != nil {
		return db.Repository{}, gitBackend{}, fmt.Errorf("lookup repo: %w", err)
	}
	if err := s.authorizeProjectMember(ctx, act, project.ID); err != nil {
		if errors.Is(err, ErrForbidden) && repo.Visibility == VisibilityPublic {
			if act.UserID == uuid.Nil {
				s.logger.Debug("anonymous public repo read", "project", projectSlug, "repo", repoSlug)
			}
		} else if errors.Is(err, ErrForbidden) && act.UserID == uuid.Nil {
			return db.Repository{}, gitBackend{}, ErrNotFound
		} else {
			return db.Repository{}, gitBackend{}, err
		}
	}
	if !requireGit {
		// Metadata-only calls (GetRepo) never touch a git backend.
		return repo, gitBackend{}, nil
	}
	_, stateErr := s.store.GetGitRepoState(ctx, repo.ID)
	switch {
	case stateErr == nil:
		if s.gitCache == nil || s.gitEngine == nil {
			// A referee row exists but the engine is not wired (config
			// regression): fail closed rather than serving stale somewhere.
			return db.Repository{}, gitBackend{}, ErrUnavailable
		}
		return repo, gitBackend{
			engine:      true,
			repoID:      repo.ID,
			tenantID:    project.TenantID,
			projectSlug: project.Slug,
			repoSlug:    repo.Slug,
		}, nil
	case errors.Is(stateErr, pgx.ErrNoRows):
		// Forgejo backend — this repo's content still lives on the VM.
		if !s.forgejoEnabled() {
			return db.Repository{}, gitBackend{}, ErrUnavailable
		}
		owner, name, ok := forgejoTarget(repo, project)
		if !ok {
			return db.Repository{}, gitBackend{}, ErrUnavailable
		}
		return repo, gitBackend{owner: owner, name: name}, nil
	default:
		return db.Repository{}, gitBackend{}, fmt.Errorf("lookup git state: %w", stateErr)
	}
}

// engineRepoEmpty reports whether an engine repo has no refs yet (freshly
// provisioned): the browse equivalent of Forgejo's empty-repo 409.
func (s *Service) engineRepoEmpty(ctx context.Context, be gitBackend) (bool, error) {
	m, _, err := s.gitEngine.LoadManifest(ctx, be.tenantID.String(), be.projectSlug, be.repoSlug)
	if err != nil {
		return false, err
	}
	return len(m.Refs) == 0, nil
}

// engineDir hydrates the repo's bare dir once per browse call.
func (s *Service) engineDir(ctx context.Context, be gitBackend) (string, error) {
	return s.gitCache.Hydrate(ctx, be.tenantID.String(), be.projectSlug, be.repoSlug)
}

// ---- shared git plumbing ----------------------------------------------------
//
// gitBrowse runs git in a hydrated bare repo and maps "unknown anything" git
// failures to ErrNotFound (Forgejo's API returns 404 for unknown refs, paths,
// and objects — the engine mirrors that).

func gitBrowse(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	var out, errOut bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errOut
	if err := cmd.Run(); err != nil {
		msg := errOut.String()
		if notFoundGitMsg(msg) {
			return "", ErrNotFound
		}
		return "", fmt.Errorf("git %v: %w: %s", args, err, msg)
	}
	return out.String(), nil
}

func notFoundGitMsg(msg string) bool {
	for _, pat := range []string{
		"unknown revision",
		"bad revision",
		"bad object",
		"does not match any",
		"did not match any",
		"does not exist in",
		"unknown object",
		"ambiguous argument",
		"not a valid object",
	} {
		if strings.Contains(msg, pat) {
			return true
		}
	}
	return false
}

// isoTime parses git's %aI/%cI ISO-8601 timestamps.
func isoTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

// logFormat is the commit-record format: \x1f separates the 8 fields, \x1e
// terminates one record (needed because %B bodies contain newlines).
const logFormat = "%H%x1f%B%x1f%an%x1f%ae%x1f%aI%x1f%cn%x1f%ce%x1f%cI%x1e"

// parseLogRecords splits a git log --format=logFormat output into raw
// records (one per commit, without the record terminators).
func parseLogRecords(out string) []string {
	var recs []string
	for _, rec := range strings.Split(out, "\x1e") {
		rec = strings.TrimPrefix(rec, "\n")
		if strings.TrimSpace(rec) == "" {
			continue
		}
		recs = append(recs, rec)
	}
	return recs
}

// commitFromRecord parses one logFormat record into the forgejo.Commit shape
// (Author/Committer user links stay nil: engine repos have no Forgejo users).
func commitFromRecord(rec string) (forgejo.Commit, error) {
	parts := strings.Split(rec, "\x1f")
	if len(parts) != 8 {
		return forgejo.Commit{}, fmt.Errorf("commit record has %d fields, want 8", len(parts))
	}
	return forgejo.Commit{
		SHA: parts[0],
		Commit: forgejo.CommitPayload{
			Message: strings.TrimRight(parts[1], "\n"),
			Author: forgejo.CommitSignature{
				Name: parts[2], Email: parts[3], Date: isoTime(parts[4]),
			},
			Committer: forgejo.CommitSignature{
				Name: parts[5], Email: parts[6], Date: isoTime(parts[7]),
			},
		},
	}, nil
}

// bareListBranches returns the branches with tip commits from a hydrated dir.
// (for-each-ref formats do not expand %xNN escapes — the separators are real
// bytes; refnames cannot contain them.)
func bareListBranches(dir string) ([]forgejo.Branch, error) {
	const f = "%(refname:short)\x1f%(objectname)\x1f%(contents:subject)\x1f%(authorname)\x1f%(authoremail)\x1f%(authordate:iso8601-strict)"
	out, err := gitBrowse(dir, "for-each-ref", "refs/heads", "--format="+f)
	if err != nil {
		return nil, err
	}
	var branches []forgejo.Branch
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		parts := strings.Split(line, "\x1f")
		if len(parts) != 6 {
			return nil, fmt.Errorf("branch record has %d fields: %q", len(parts), line)
		}
		branches = append(branches, forgejo.Branch{
			Name: parts[0],
			Commit: forgejo.BranchCommit{
				ID:        parts[1],
				Message:   parts[2],
				Author:    forgejo.GitActor{Name: parts[3], Email: strings.Trim(parts[4], "<>")},
				Timestamp: isoTime(parts[5]),
			},
		})
	}
	return branches, nil
}

// bareListCommits mirrors the forgejo client's ListCommits semantics: limit
// defaults to 30, max 100; a path filters; refs may be branch/tag/sha.
func bareListCommits(dir, ref, path string, limit int) ([]forgejo.Commit, error) {
	if limit <= 0 || limit > 100 {
		limit = 30
	}
	args := []string{"log", "-n", strconv.Itoa(limit), "--format=" + logFormat, ref}
	if path != "" {
		args = append(args, "--", path)
	}
	out, err := gitBrowse(dir, args...)
	if err != nil {
		return nil, err
	}
	var commits []forgejo.Commit
	for _, rec := range parseLogRecords(out) {
		c, err := commitFromRecord(rec)
		if err != nil {
			return nil, err
		}
		commits = append(commits, c)
	}
	return commits, nil
}

// bareGetCommit returns a single commit's metadata (diff fetched separately,
// like the forgejo client).
func bareGetCommit(dir, sha string) (forgejo.Commit, error) {
	out, err := gitBrowse(dir, "show", "-s", "--format="+logFormat, sha)
	if err != nil {
		return forgejo.Commit{}, err
	}
	recs := parseLogRecords(out)
	if len(recs) == 0 {
		return forgejo.Commit{}, ErrNotFound
	}
	return commitFromRecord(recs[0])
}

// bareCommitDiff returns the unified diff of a commit against its first
// parent (root commits diff against the empty tree), capped byte-identically
// to the forgejo client's 2 MiB read limit.
func bareCommitDiff(dir, sha string) (string, error) {
	cmd := exec.Command("git", "diff-tree", "-p", "--root", "--no-commit-id", sha)
	cmd.Dir = dir
	var out, errOut bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errOut
	if err := cmd.Run(); err != nil {
		if notFoundGitMsg(errOut.String()) {
			return "", ErrNotFound
		}
		return "", fmt.Errorf("git diff-tree %s: %w: %s", sha, err, errOut.String())
	}
	b := out.Bytes()
	if int64(len(b)) > browseDiffBytes {
		b = b[:browseDiffBytes]
	}
	return string(b), nil
}

// bareGetContents mirrors the forgejo contents endpoint: directory listing
// or single file (base64) at path/ref.
func bareGetContents(dir, path, ref string) (*forgejo.Contents, error) {
	path = strings.Trim(path, "/")
	if path == "" {
		return lsTreeEntries(dir, ref, "")
	}
	objType, err := gitBrowse(dir, "cat-file", "-t", ref+":"+path)
	if err != nil {
		return nil, err
	}
	switch strings.TrimSpace(objType) {
	case "tree":
		return lsTreeEntries(dir, ref+":"+path, path+"/")
	case "blob":
		return blobContents(dir, ref+":"+path, path)
	default:
		// Direct submodule/etc. paths are not content-fetchable.
		return nil, ErrNotFound
	}
}

// lsTreeEntries lists one tree. treeish is a ref or ref:dir; prefix is "" for
// the repo root, "dir/" for subdirectories (entry paths carry the full
// repo-relative path, like the forgejo payload).
func lsTreeEntries(dir, treeish, prefix string) (*forgejo.Contents, error) {
	out, err := gitBrowse(dir, "ls-tree", "-l", "--full-name", treeish)
	if err != nil {
		return nil, err
	}
	contents := &forgejo.Contents{IsDir: true}
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		entry, err := lsTreeEntry(line, prefix)
		if err != nil {
			return nil, err
		}
		contents.Entries = append(contents.Entries, entry)
	}
	return contents, nil
}

func lsTreeEntry(line, prefix string) (forgejo.ContentEntry, error) {
	tab := strings.IndexByte(line, '\t')
	if tab < 0 {
		return forgejo.ContentEntry{}, fmt.Errorf("ls-tree line without tab: %q", line)
	}
	full := line[:tab]
	name := line[tab+1:]
	fields := strings.Fields(full)
	if len(fields) < 4 {
		return forgejo.ContentEntry{}, fmt.Errorf("ls-tree meta %q: %d fields", full, len(fields))
	}
	mode, typ, sha, size := fields[0], fields[1], fields[2], fields[3]
	e := forgejo.ContentEntry{
		Name: filepath.Base(name),
		Path: prefix + name,
		SHA:  sha,
	}
	sz, _ := strconv.ParseInt(size, 10, 64)
	e.Size = sz
	switch {
	case typ == "tree":
		e.Type = "dir"
	case typ == "commit":
		e.Type = "submodule"
	case mode == "120000":
		e.Type = "symlink"
	default:
		e.Type = "file"
	}
	return e, nil
}

// blobContents fetches one file as base64, mirroring the forgejo file payload.
func blobContents(dir, object, path string) (*forgejo.Contents, error) {
	cmd := exec.Command("git", "cat-file", "blob", object)
	cmd.Dir = dir
	var out, errOut bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errOut
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("git cat-file %s: %w: %s", object, err, errOut.String())
	}
	shaOut, err := gitBrowse(dir, "rev-parse", object)
	if err != nil {
		return nil, err
	}
	body := out.Bytes()
	enc := "base64"
	content := base64.StdEncoding.EncodeToString(body)
	e := forgejo.ContentEntry{
		Name:     filepath.Base(path),
		Path:     path,
		SHA:      strings.TrimSpace(shaOut),
		Type:     "file",
		Size:     int64(len(body)),
		Encoding: &enc,
		Content:  &content,
	}
	return &forgejo.Contents{File: &e}, nil
}

// bareLastCommit returns the latest commit touching path at ref — the
// engine-side equivalent of the per-entry ListCommits(path, 1) lookup.
func bareLastCommit(dir, ref, path string) (*forgejo.EntryCommit, error) {
	out, err := gitBrowse(dir, "log", "-n", "1", "--format="+logFormat, ref, "--", path)
	if err != nil {
		return nil, err
	}
	recs := parseLogRecords(out)
	if len(recs) == 0 {
		return nil, nil
	}
	c, err := commitFromRecord(recs[0])
	if err != nil {
		return nil, err
	}
	return &forgejo.EntryCommit{
		SHA:        c.SHA,
		Message:    c.Commit.Message,
		AuthorName: c.Commit.Author.Name,
		Date:       c.Commit.Author.Date,
	}, nil
}
