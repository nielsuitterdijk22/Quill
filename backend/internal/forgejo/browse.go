package forgejo

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// This file adds the read-only operations Quill needs to browse repository code:
// branches, commits, and directory/file contents. They wrap the corresponding
// Forgejo REST endpoints and are driven by the admin token, so Quill mediates
// every read (visibility is enforced in the platform layer, not here).

// ---- types -----------------------------------------------------------------

// GitActor is the name/email pair recorded in a git commit.
type GitActor struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

// BranchCommit is the tip commit summary returned with a branch.
type BranchCommit struct {
	ID        string    `json:"id"`
	Message   string    `json:"message"`
	Timestamp time.Time `json:"timestamp"`
	Author    GitActor  `json:"author"`
}

// Branch is a git branch with its tip commit.
type Branch struct {
	Name      string       `json:"name"`
	Commit    BranchCommit `json:"commit"`
	Protected bool         `json:"protected"`
}

// CommitSignature is an author/committer entry inside a commit payload.
type CommitSignature struct {
	Name  string    `json:"name"`
	Email string    `json:"email"`
	Date  time.Time `json:"date"`
}

// CommitPayload is the git-level commit metadata.
type CommitPayload struct {
	Message   string          `json:"message"`
	Author    CommitSignature `json:"author"`
	Committer CommitSignature `json:"committer"`
}

// Commit is one entry from the commit log. Author/Committer are the linked
// Forgejo users when known (they may be nil for commits without a matching user).
type Commit struct {
	SHA       string        `json:"sha"`
	HTMLURL   string        `json:"html_url"`
	Commit    CommitPayload `json:"commit"`
	Author    *User         `json:"author"`
	Committer *User         `json:"committer"`
}

// EntryCommit is the last commit that touched a directory entry. It is populated
// by Quill via a per-path commit lookup and is not part of the Forgejo contents
// payload, so it carries no JSON tags for unmarshaling.
type EntryCommit struct {
	SHA         string
	Message     string
	AuthorName  string
	AuthorLogin string
	Date        time.Time
}

// ContentEntry is a single file or directory in a repository tree. For a file
// fetched directly, Encoding is "base64" and Content holds the encoded bytes;
// directory listings leave Content nil.
type ContentEntry struct {
	Name        string  `json:"name"`
	Path        string  `json:"path"`
	SHA         string  `json:"sha"`
	Type        string  `json:"type"` // file | dir | symlink | submodule
	Size        int64   `json:"size"`
	Encoding    *string `json:"encoding"`
	Content     *string `json:"content"`
	DownloadURL *string `json:"download_url"`

	// LastCommit is the most recent commit touching this entry. The Forgejo
	// contents endpoint does not report it, so Quill fills it in for directory
	// listings (see platform.GetContents); it is nil when unknown.
	LastCommit *EntryCommit `json:"-"`
}

// Contents is a discriminated result from the contents endpoint: a directory
// listing (IsDir) or a single file.
type Contents struct {
	IsDir   bool
	Entries []ContentEntry
	File    *ContentEntry
}

// ---- operations ------------------------------------------------------------

// ListBranches returns the repository's branches with their tip commits.
func (c *Client) ListBranches(ctx context.Context, owner, repo string) ([]Branch, error) {
	var out []Branch
	path := "/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(repo) + "/branches?limit=100"
	err := c.do(ctx, http.MethodGet, path, nil, &out)
	return out, err
}

// ListCommits returns up to limit commits reachable from ref (a branch, tag, or
// SHA). When path is non-empty only commits touching that path are returned.
func (c *Client) ListCommits(ctx context.Context, owner, repo, ref, path string, limit int) ([]Commit, error) {
	if limit <= 0 || limit > 100 {
		limit = 30
	}
	q := url.Values{}
	q.Set("limit", strconv.Itoa(limit))
	q.Set("stat", "false")
	q.Set("verification", "false")
	q.Set("files", "false")
	if ref != "" {
		q.Set("sha", ref)
	}
	if path != "" {
		q.Set("path", path)
	}
	var out []Commit
	p := "/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(repo) + "/commits?" + q.Encode()
	err := c.do(ctx, http.MethodGet, p, nil, &out)
	return out, err
}

// GetCommit returns a single commit's metadata by SHA (or any ref that resolves
// to one). The diff is fetched separately via GetCommitDiff, so the heavy file
// list is suppressed here.
func (c *Client) GetCommit(ctx context.Context, owner, repo, sha string) (Commit, error) {
	var out Commit
	p := "/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(repo) +
		"/git/commits/" + url.PathEscape(sha) + "?stat=false&verification=false&files=false"
	err := c.do(ctx, http.MethodGet, p, nil, &out)
	return out, err
}

// GetCommitDiff returns the unified diff text introduced by a single commit
// (the change against its first parent).
func (c *Client) GetCommitDiff(ctx context.Context, owner, repo, sha string) (string, error) {
	p := "/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(repo) +
		"/git/commits/" + url.PathEscape(sha) + ".diff"
	raw, err := c.getRaw(ctx, p, maxDiffBytes)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// GetContents fetches a path within a repository at ref (empty ref = default
// branch). It returns a directory listing or a single file with base64 content.
func (c *Client) GetContents(ctx context.Context, owner, repo, path, ref string) (*Contents, error) {
	p := "/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(repo) + "/contents"
	if clean := strings.Trim(path, "/"); clean != "" {
		p += "/" + escapeContentsPath(clean)
	}
	if ref != "" {
		p += "?ref=" + url.QueryEscape(ref)
	}

	var raw json.RawMessage
	if err := c.do(ctx, http.MethodGet, p, nil, &raw); err != nil {
		return nil, err
	}
	// The endpoint returns an array for directories and an object for files.
	if trimmed := bytes.TrimSpace(raw); len(trimmed) > 0 && trimmed[0] == '[' {
		var entries []ContentEntry
		if err := json.Unmarshal(raw, &entries); err != nil {
			return nil, err
		}
		return &Contents{IsDir: true, Entries: entries}, nil
	}
	var file ContentEntry
	if err := json.Unmarshal(raw, &file); err != nil {
		return nil, err
	}
	return &Contents{File: &file}, nil
}

// escapeContentsPath path-escapes each segment of a repo-relative path while
// preserving the slashes between segments.
func escapeContentsPath(p string) string {
	segments := strings.Split(p, "/")
	for i, s := range segments {
		segments[i] = url.PathEscape(s)
	}
	return strings.Join(segments, "/")
}
