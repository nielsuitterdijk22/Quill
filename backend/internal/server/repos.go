package server

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"

	"github.com/nielsuitterdijk22/quill/internal/forgejo"
	"github.com/nielsuitterdijk22/quill/internal/httpx"
	"github.com/nielsuitterdijk22/quill/internal/platform"
)

// This file holds the read-only repository browsing endpoints added in PR 5:
// repo detail, branches, commits, and directory/file contents. They translate
// the platform service's Forgejo-backed reads into clean JSON for the frontend.

// maxInlineFileBytes caps how large a file may be before the API stops returning
// its decoded contents (the frontend shows a "too large to display" notice).
const maxInlineFileBytes = 512 << 10

// branchResponse is the public JSON shape for a git branch.
type branchResponse struct {
	Name          string    `json:"name"`
	Protected     bool      `json:"protected"`
	CommitSHA     string    `json:"commitSha"`
	CommitMessage string    `json:"commitMessage"`
	CommitDate    time.Time `json:"commitDate"`
}

// commitResponse is the public JSON shape for a commit log entry.
type commitResponse struct {
	SHA         string    `json:"sha"`
	Message     string    `json:"message"`
	AuthorName  string    `json:"authorName"`
	AuthorLogin string    `json:"authorLogin,omitempty"`
	Date        time.Time `json:"date"`
}

// contentEntryResponse is one entry in a directory listing.
type contentEntryResponse struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Type string `json:"type"`
	Size int64  `json:"size"`
}

// contentFileResponse is a single file's metadata plus its decoded text content
// when the file is textual and small enough to inline.
type contentFileResponse struct {
	Name     string `json:"name"`
	Path     string `json:"path"`
	SHA      string `json:"sha"`
	Size     int64  `json:"size"`
	IsBinary bool   `json:"isBinary"`
	TooLarge bool   `json:"tooLarge"`
	Content  string `json:"content,omitempty"`
}

// contentsResponse is the discriminated result of the contents endpoint.
type contentsResponse struct {
	Type    string                 `json:"type"` // "dir" | "file"
	Path    string                 `json:"path"`
	Entries []contentEntryResponse `json:"entries,omitempty"`
	File    *contentFileResponse   `json:"file,omitempty"`
}

// handleGetRepo returns a single repository's metadata.
func (s *Server) handleGetRepo(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	repo, err := s.platform.GetRepo(r.Context(), actor, chi.URLParam(r, "slug"), chi.URLParam(r, "repo"))
	if err != nil {
		s.writePlatformError(w, err, "could not load repository")
		return
	}
	resp := newRepoResponse(repo)
	if info, err := s.platform.GetRepoStarInfo(r.Context(), actor, repo); err == nil {
		resp.StarCount = info.StarCount
		resp.ViewerHasStarred = info.ViewerHasStarred
	}
	httpx.JSON(w, http.StatusOK, resp)
}

// handleStarRepo records that the authenticated user has starred a repository.
func (s *Server) handleStarRepo(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	if err := s.platform.StarRepo(r.Context(), actor, chi.URLParam(r, "slug"), chi.URLParam(r, "repo")); err != nil {
		s.writePlatformError(w, err, "could not star repository")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleUnstarRepo removes the authenticated user's star from a repository.
func (s *Server) handleUnstarRepo(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	if err := s.platform.UnstarRepo(r.Context(), actor, chi.URLParam(r, "slug"), chi.URLParam(r, "repo")); err != nil {
		s.writePlatformError(w, err, "could not unstar repository")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// updateRepoRequest is the partial update body for a repository's settings. Every
// field is a pointer so an omitted field is left unchanged (distinct from a field
// explicitly set to its zero value, e.g. clearing the description).
type updateRepoRequest struct {
	Name          *string `json:"name"`
	Description   *string `json:"description"`
	Visibility    *string `json:"visibility"`
	DefaultBranch *string `json:"defaultBranch"`
	Slug          *string `json:"slug"`
	Archived      *bool   `json:"archived"`
}

// handleUpdateRepo changes a repository's general settings (project owners / admins).
func (s *Server) handleUpdateRepo(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	var req updateRepoRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	repo, err := s.platform.UpdateRepo(r.Context(), actor, chi.URLParam(r, "slug"), chi.URLParam(r, "repo"), platform.UpdateRepoInput{
		Name:          req.Name,
		Description:   req.Description,
		Visibility:    req.Visibility,
		DefaultBranch: req.DefaultBranch,
		Slug:          req.Slug,
		Archived:      req.Archived,
	})
	if err != nil {
		s.writePlatformError(w, err, "could not update repository")
		return
	}
	meta := map[string]any{"project": chi.URLParam(r, "slug"), "slug": repo.Slug}
	if req.Visibility != nil {
		meta["visibility"] = *req.Visibility
		s.logAudit(r, "repo.visibility_changed", "repository", repo.ID.String(), meta)
	} else if req.Archived != nil {
		if *req.Archived {
			s.logAudit(r, "repo.archived", "repository", repo.ID.String(), meta)
		} else {
			s.logAudit(r, "repo.unarchived", "repository", repo.ID.String(), meta)
		}
	}
	httpx.JSON(w, http.StatusOK, newRepoResponse(repo))
}

// handleDeleteRepo permanently deletes a repository (project owners / admins).
func (s *Server) handleDeleteRepo(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	project, repoSlug := chi.URLParam(r, "slug"), chi.URLParam(r, "repo")
	if err := s.platform.DeleteRepo(r.Context(), actor, project, repoSlug); err != nil {
		s.writePlatformError(w, err, "could not delete repository")
		return
	}
	s.logAudit(r, "repo.deleted", "repository", repoSlug, map[string]any{
		"slug":    repoSlug,
		"project": project,
	})
	w.WriteHeader(http.StatusNoContent)
}

type forkRepoRequest struct {
	TargetProject string `json:"targetProject"` // slug of the project to fork into
	Slug          string `json:"slug"`          // desired slug for the forked repo
}

// handleForkRepo forks a repository into a different (or the same) project.
func (s *Server) handleForkRepo(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	var req forkRepoRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	repo, err := s.platform.ForkRepo(r.Context(), actor, chi.URLParam(r, "slug"), chi.URLParam(r, "repo"), platform.ForkRepoInput{
		TargetProjectSlug: req.TargetProject,
		Slug:              req.Slug,
	})
	if err != nil {
		s.writePlatformError(w, err, "could not fork repository")
		return
	}
	s.logAudit(r, "repo.forked", "repository", repo.ID.String(), map[string]any{
		"source_project": chi.URLParam(r, "slug"),
		"source_repo":    chi.URLParam(r, "repo"),
		"target_project": req.TargetProject,
		"fork_slug":      repo.Slug,
	})
	httpx.JSON(w, http.StatusCreated, map[string]any{"repository": newRepoResponse(repo)})
}

// handleListBranches returns a repository's git branches.
func (s *Server) handleListBranches(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	repo, branches, err := s.platform.ListBranches(r.Context(), actor, chi.URLParam(r, "slug"), chi.URLParam(r, "repo"))
	if err != nil {
		s.writePlatformError(w, err, "could not list branches")
		return
	}
	out := make([]branchResponse, 0, len(branches))
	for _, b := range branches {
		out = append(out, branchResponse{
			Name:          b.Name,
			Protected:     b.Protected,
			CommitSHA:     b.Commit.ID,
			CommitMessage: b.Commit.Message,
			CommitDate:    b.Commit.Timestamp,
		})
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"repository":    newRepoResponse(repo),
		"defaultBranch": repo.DefaultBranch,
		"branches":      out,
	})
}

// handleListCommits returns a repository's commit log at an optional ref/path.
func (s *Server) handleListCommits(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	repo, commits, err := s.platform.ListCommits(r.Context(), actor, chi.URLParam(r, "slug"), chi.URLParam(r, "repo"), q.Get("ref"), q.Get("path"), limit)
	if err != nil {
		s.writePlatformError(w, err, "could not list commits")
		return
	}
	out := make([]commitResponse, 0, len(commits))
	for _, c := range commits {
		entry := commitResponse{
			SHA:        c.SHA,
			Message:    c.Commit.Message,
			AuthorName: c.Commit.Author.Name,
			Date:       c.Commit.Author.Date,
		}
		if c.Author != nil {
			entry.AuthorLogin = c.Author.Login
		}
		out = append(out, entry)
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"repository": newRepoResponse(repo),
		"commits":    out,
	})
}

// handleGetCommit returns a single commit's metadata and its parsed diff.
func (s *Server) handleGetCommit(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	repo, commit, files, err := s.platform.GetCommit(r.Context(), actor, chi.URLParam(r, "slug"), chi.URLParam(r, "repo"), chi.URLParam(r, "sha"))
	if err != nil {
		s.writePlatformError(w, err, "could not load commit")
		return
	}
	entry := commitResponse{
		SHA:        commit.SHA,
		Message:    commit.Commit.Message,
		AuthorName: commit.Commit.Author.Name,
		Date:       commit.Commit.Author.Date,
	}
	if commit.Author != nil {
		entry.AuthorLogin = commit.Author.Login
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"repository": newRepoResponse(repo),
		"commit":     entry,
		"files":      newDiffFiles(files),
	})
}

// handleGetContents returns a directory listing or a single file at path/ref.
func (s *Server) handleGetContents(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	q := r.URL.Query()
	path := q.Get("path")
	if !isValidRepoPath(path) {
		httpx.Error(w, http.StatusBadRequest, "invalid_path", "path contains invalid segments")
		return
	}
	repo, contents, err := s.platform.GetContents(r.Context(), actor, chi.URLParam(r, "slug"), chi.URLParam(r, "repo"), path, q.Get("ref"))
	if err != nil {
		s.writePlatformError(w, err, "could not load contents")
		return
	}

	resp := contentsResponse{Path: path}
	if contents.IsDir {
		resp.Type = "dir"
		resp.Entries = newContentEntries(contents.Entries)
	} else {
		resp.Type = "file"
		resp.File = newContentFile(contents.File)
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"repository": newRepoResponse(repo),
		"contents":   resp,
	})
}

// renderMarkupRequest carries raw markdown to render. maxMarkupInputBytes caps
// it so a request can't ask Forgejo to render an unbounded blob.
type renderMarkupRequest struct {
	Text string `json:"text"`
}

const maxMarkupInputBytes = 512 << 10

// handleRenderMarkup renders markdown text to sanitized HTML in the repository's
// context (so relative links and references resolve), for the README and other
// markdown the frontend displays.
func (s *Server) handleRenderMarkup(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	// READMEs can be larger than the small auth-body cap, so decode with the
	// markup-specific limit (plus slack for JSON overhead) rather than decodeJSON.
	r.Body = http.MaxBytesReader(w, r.Body, maxMarkupInputBytes+(4<<10))
	var req renderMarkupRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid_body", "request body must be valid JSON")
		return
	}
	if len(req.Text) > maxMarkupInputBytes {
		httpx.Error(w, http.StatusBadRequest, "invalid_input", "markdown input is too large to render")
		return
	}
	projectSlug := chi.URLParam(r, "slug")
	repoSlug := chi.URLParam(r, "repo")
	cacheKey := markupCacheKey(projectSlug, repoSlug, req.Text)
	if cached, ok := s.markupCache.get(cacheKey); ok {
		httpx.JSON(w, http.StatusOK, map[string]any{"html": cached})
		return
	}
	html, err := s.platform.RenderMarkdown(r.Context(), actor, projectSlug, repoSlug, req.Text)
	if err != nil {
		s.writePlatformError(w, err, "could not render markdown")
		return
	}
	s.markupCache.set(cacheKey, html)
	httpx.JSON(w, http.StatusOK, map[string]any{"html": html})
}

// newContentEntries converts and sorts directory entries: directories first,
// then files, each alphabetically (case-insensitive) for a stable tree view.
func newContentEntries(entries []forgejo.ContentEntry) []contentEntryResponse {
	out := make([]contentEntryResponse, 0, len(entries))
	for _, e := range entries {
		out = append(out, contentEntryResponse{
			Name: e.Name,
			Path: e.Path,
			Type: e.Type,
			Size: e.Size,
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		di, dj := out[i].Type == "dir", out[j].Type == "dir"
		if di != dj {
			return di
		}
		return lowerName(out[i].Name) < lowerName(out[j].Name)
	})
	return out
}

// newContentFile builds a file response, decoding the base64 payload into text
// when the file is small enough and valid UTF-8; binary or oversized files carry
// flags instead of content.
func newContentFile(e *forgejo.ContentEntry) *contentFileResponse {
	f := &contentFileResponse{
		Name: e.Name,
		Path: e.Path,
		SHA:  e.SHA,
		Size: e.Size,
	}
	if e.Size > maxInlineFileBytes {
		f.TooLarge = true
		return f
	}
	if e.Content == nil {
		return f
	}
	raw, err := base64.StdEncoding.DecodeString(*e.Content)
	if err != nil {
		f.IsBinary = true
		return f
	}
	if !utf8.Valid(raw) {
		f.IsBinary = true
		return f
	}
	f.Content = string(raw)
	return f
}

// isValidRepoPath reports whether a repo-relative path is safe to forward to
// Forgejo. It rejects dot-dot and lone-dot segments that could traverse above
// the repository root when a Go HTTP router normalises the URL before routing.
// An empty path is allowed and maps to the repository root.
func isValidRepoPath(p string) bool {
	if p == "" {
		return true
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == ".." || seg == "." || seg == "" {
			return false
		}
	}
	return true
}

func lowerName(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + ('a' - 'A')
		}
	}
	return string(b)
}
