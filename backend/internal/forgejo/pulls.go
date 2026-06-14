package forgejo

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// This file wraps Forgejo's pull-request surface: listing, reading, creating,
// commenting on, and merging pull requests, plus fetching the unified diff and
// parsing it into a structured form the frontend can render. Writes that should
// be attributed to a Quill user are issued with Forgejo's "sudo" facility (see
// CreatePull/CreateIssueComment/MergePull) so the acting user — not the admin
// token — is recorded as the author.

// maxDiffBytes caps the size of a unified diff Quill will fetch and parse.
const maxDiffBytes = 2 << 20 // 2 MiB

// ---- types -----------------------------------------------------------------

// PRRef is one side (head or base) of a pull request.
type PRRef struct {
	Label string `json:"label"`
	Ref   string `json:"ref"`
	SHA   string `json:"sha"`
}

// PullRequest is a Forgejo pull request.
type PullRequest struct {
	Number       int        `json:"number"`
	Title        string     `json:"title"`
	Body         string     `json:"body"`
	State        string     `json:"state"` // open | closed
	Draft        bool       `json:"draft"`
	Merged       bool       `json:"merged"`
	Mergeable    bool       `json:"mergeable"`
	Comments     int        `json:"comments"`
	Additions    int        `json:"additions"`
	Deletions    int        `json:"deletions"`
	ChangedFiles int        `json:"changed_files"`
	User         *User      `json:"user"`
	Head         PRRef      `json:"head"`
	Base         PRRef      `json:"base"`
	HTMLURL      string     `json:"html_url"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
	MergedAt     *time.Time `json:"merged_at"`
	MergedBy     *User      `json:"merged_by"`
	MergeCommit  string     `json:"merge_commit_sha"`
}

// IssueComment is a comment on a pull request's conversation thread.
type IssueComment struct {
	ID        int64     `json:"id"`
	Body      string    `json:"body"`
	User      *User     `json:"user"`
	HTMLURL   string    `json:"html_url"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// CreatePullOptions describes a new pull request.
type CreatePullOptions struct {
	Title string `json:"title"`
	Body  string `json:"body,omitempty"`
	Head  string `json:"head"`
	Base  string `json:"base"`
}

// MergePullOptions describes how to merge a pull request. Method is one of
// "merge", "squash", or "rebase".
type MergePullOptions struct {
	Do                string `json:"Do"`
	MergeTitleField   string `json:"MergeTitleField,omitempty"`
	MergeMessageField string `json:"MergeMessageField,omitempty"`
	DeleteBranch      bool   `json:"delete_branch_after_merge,omitempty"`
}

// ---- diff types ------------------------------------------------------------

// DiffLineType classifies a line within a diff hunk.
const (
	DiffLineContext = "context"
	DiffLineAdd     = "add"
	DiffLineDel     = "del"
)

// DiffLine is a single line of a hunk with its old/new line numbers (0 when the
// line does not exist on that side).
type DiffLine struct {
	Type      string
	Content   string
	OldNumber int
	NewNumber int
}

// DiffHunk is a contiguous block of changes preceded by an @@ header.
type DiffHunk struct {
	Header string
	Lines  []DiffLine
}

// DiffFile is the set of changes to a single file.
type DiffFile struct {
	OldPath   string
	NewPath   string
	Path      string
	Status    string // added | deleted | renamed | modified
	IsBinary  bool
	Additions int
	Deletions int
	Hunks     []DiffHunk
}

// ---- operations ------------------------------------------------------------

// ListPulls returns pull requests for a repository. state is "open", "closed",
// or "all".
func (c *Client) ListPulls(ctx context.Context, owner, repo, state string, limit int) ([]PullRequest, error) {
	if state == "" {
		state = "open"
	}
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	q := url.Values{}
	q.Set("state", state)
	q.Set("limit", strconv.Itoa(limit))
	q.Set("sort", "recentupdate")
	var out []PullRequest
	p := "/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(repo) + "/pulls?" + q.Encode()
	err := c.do(ctx, http.MethodGet, p, nil, &out)
	return out, err
}

// GetPull returns a single pull request by its number.
func (c *Client) GetPull(ctx context.Context, owner, repo string, number int) (PullRequest, error) {
	var out PullRequest
	p := "/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(repo) + "/pulls/" + strconv.Itoa(number)
	err := c.do(ctx, http.MethodGet, p, nil, &out)
	return out, err
}

// CreatePull opens a pull request. When asUser is non-empty the request is
// attributed to that Forgejo user via sudo (the user must have access to the
// repository — see platform.ensureCollaborator).
func (c *Client) CreatePull(ctx context.Context, owner, repo, asUser string, opts CreatePullOptions) (PullRequest, error) {
	var out PullRequest
	p := "/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(repo) + "/pulls" + sudoQuery(asUser)
	err := c.do(ctx, http.MethodPost, p, opts, &out)
	return out, err
}

// GetPullDiff returns the unified diff text for a pull request.
func (c *Client) GetPullDiff(ctx context.Context, owner, repo string, number int) (string, error) {
	p := "/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(repo) + "/pulls/" + strconv.Itoa(number) + ".diff"
	raw, err := c.getRaw(ctx, p, maxDiffBytes)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// ListIssueComments returns the conversation comments on a pull request (PRs and
// issues share the comment endpoint in Forgejo).
func (c *Client) ListIssueComments(ctx context.Context, owner, repo string, number int) ([]IssueComment, error) {
	var out []IssueComment
	p := "/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(repo) + "/issues/" + strconv.Itoa(number) + "/comments"
	err := c.do(ctx, http.MethodGet, p, nil, &out)
	return out, err
}

// CreateIssueComment adds a comment to a pull request, attributed to asUser when
// non-empty.
func (c *Client) CreateIssueComment(ctx context.Context, owner, repo string, number int, asUser, body string) (IssueComment, error) {
	var out IssueComment
	p := "/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(repo) + "/issues/" + strconv.Itoa(number) + "/comments" + sudoQuery(asUser)
	err := c.do(ctx, http.MethodPost, p, map[string]string{"body": body}, &out)
	return out, err
}

// MergePull merges a pull request, attributed to asUser when non-empty.
func (c *Client) MergePull(ctx context.Context, owner, repo string, number int, asUser string, opts MergePullOptions) error {
	p := "/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(repo) + "/pulls/" + strconv.Itoa(number) + "/merge" + sudoQuery(asUser)
	return c.do(ctx, http.MethodPost, p, opts, nil)
}

// AddCollaborator grants a user access to a repository with the given permission
// ("read", "write", or "admin"). It is idempotent. Quill uses it to grant the
// access Forgejo requires before a sudo'd write is attributed to a user.
func (c *Client) AddCollaborator(ctx context.Context, owner, repo, username, permission string) error {
	p := "/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(repo) + "/collaborators/" + url.PathEscape(username)
	return c.do(ctx, http.MethodPut, p, map[string]string{"permission": permission}, nil)
}

// CreateBranch creates a new branch from an existing one.
func (c *Client) CreateBranch(ctx context.Context, owner, repo, newBranch, fromBranch string) error {
	p := "/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(repo) + "/branches"
	body := map[string]string{"new_branch_name": newBranch, "old_branch_name": fromBranch}
	return c.do(ctx, http.MethodPost, p, body, nil)
}

// sudoQuery returns a "?sudo=user" suffix when user is non-empty.
func sudoQuery(user string) string {
	if user == "" {
		return ""
	}
	return "?sudo=" + url.QueryEscape(user)
}

// ---- unified diff parsing --------------------------------------------------

// ParseUnifiedDiff parses git's unified diff output into per-file hunks with
// resolved old/new line numbers. It is tolerant of new/deleted/binary files and
// "\ No newline at end of file" markers.
func ParseUnifiedDiff(diff string) []DiffFile {
	var files []DiffFile
	var cur *DiffFile
	var hunk *DiffHunk
	oldNo, newNo := 0, 0

	flushHunk := func() {
		if cur != nil && hunk != nil {
			cur.Hunks = append(cur.Hunks, *hunk)
			hunk = nil
		}
	}
	flushFile := func() {
		flushHunk()
		if cur != nil {
			finalizeStatus(cur)
			files = append(files, *cur)
			cur = nil
		}
	}

	for _, line := range strings.Split(diff, "\n") {
		switch {
		case strings.HasPrefix(line, "diff --git "):
			flushFile()
			cur = &DiffFile{}
		case cur == nil:
			// Preamble before the first file header; ignore.
			continue
		case strings.HasPrefix(line, "--- "):
			cur.OldPath = stripDiffPath(line[4:])
		case strings.HasPrefix(line, "+++ "):
			cur.NewPath = stripDiffPath(line[4:])
		case strings.HasPrefix(line, "Binary files "):
			cur.IsBinary = true
		case strings.HasPrefix(line, "@@"):
			flushHunk()
			os, ns := parseHunkHeader(line)
			oldNo, newNo = os, ns
			hunk = &DiffHunk{Header: line}
		case hunk == nil:
			// File-level metadata (index, mode, rename lines); ignore.
			continue
		case strings.HasPrefix(line, "\\"):
			// "\ No newline at end of file" — not a content line.
			continue
		case strings.HasPrefix(line, "+"):
			hunk.Lines = append(hunk.Lines, DiffLine{Type: DiffLineAdd, Content: line[1:], NewNumber: newNo})
			newNo++
			cur.Additions++
		case strings.HasPrefix(line, "-"):
			hunk.Lines = append(hunk.Lines, DiffLine{Type: DiffLineDel, Content: line[1:], OldNumber: oldNo})
			oldNo++
			cur.Deletions++
		case strings.HasPrefix(line, " "):
			hunk.Lines = append(hunk.Lines, DiffLine{Type: DiffLineContext, Content: line[1:], OldNumber: oldNo, NewNumber: newNo})
			oldNo++
			newNo++
		}
	}
	flushFile()
	return files
}

// finalizeStatus derives a file's display path and change status from its
// old/new paths.
func finalizeStatus(f *DiffFile) {
	switch {
	case f.OldPath == "/dev/null":
		f.Status = "added"
		f.Path = f.NewPath
	case f.NewPath == "/dev/null":
		f.Status = "deleted"
		f.Path = f.OldPath
	case f.OldPath != "" && f.NewPath != "" && f.OldPath != f.NewPath:
		f.Status = "renamed"
		f.Path = f.NewPath
	default:
		f.Status = "modified"
		if f.NewPath != "" {
			f.Path = f.NewPath
		} else {
			f.Path = f.OldPath
		}
	}
}

// stripDiffPath removes the a/ or b/ prefix from a diff header path, preserving
// the /dev/null sentinel.
func stripDiffPath(p string) string {
	p = strings.TrimSpace(p)
	if p == "/dev/null" {
		return p
	}
	if len(p) > 2 && (p[:2] == "a/" || p[:2] == "b/") {
		return p[2:]
	}
	return p
}

// parseHunkHeader extracts the old and new starting line numbers from an
// "@@ -a,b +c,d @@" header.
func parseHunkHeader(h string) (oldStart, newStart int) {
	// h looks like: @@ -12,7 +12,9 @@ optional section heading
	body := h
	if i := strings.Index(body, "@@"); i >= 0 {
		body = body[i+2:]
	}
	if i := strings.Index(body, "@@"); i >= 0 {
		body = body[:i]
	}
	for _, tok := range strings.Fields(body) {
		switch {
		case strings.HasPrefix(tok, "-"):
			oldStart = leadingInt(tok[1:])
		case strings.HasPrefix(tok, "+"):
			newStart = leadingInt(tok[1:])
		}
	}
	return oldStart, newStart
}

// leadingInt parses the integer before an optional ",count" suffix.
func leadingInt(s string) int {
	if i := strings.IndexByte(s, ','); i >= 0 {
		s = s[:i]
	}
	n, _ := strconv.Atoi(s)
	return n
}
