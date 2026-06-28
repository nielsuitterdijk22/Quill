package forgejo

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// Issue is a Forgejo issue (distinct from a pull request).
type Issue struct {
	Number    int64     `json:"number"`
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	State     string    `json:"state"` // "open" | "closed"
	User      *User     `json:"user,omitempty"`
	Labels    []Label   `json:"labels"`
	Comments  int       `json:"comments"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Label is a Forgejo issue label.
type Label struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color"`
}

// ListIssues fetches a page of issues from a repository. state is "open",
// "closed", or "all". page is 1-indexed.
func (c *Client) ListIssues(ctx context.Context, owner, repo, state string, page int) ([]Issue, error) {
	q := url.Values{}
	q.Set("type", "issues")
	q.Set("state", state)
	q.Set("page", strconv.Itoa(page))
	q.Set("limit", "30")
	path := fmt.Sprintf("/repos/%s/%s/issues?%s",
		url.PathEscape(owner), url.PathEscape(repo), q.Encode())
	var out []Issue
	if err := c.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetIssue fetches a single issue by number.
func (c *Client) GetIssue(ctx context.Context, owner, repo string, number int64) (Issue, error) {
	path := fmt.Sprintf("/repos/%s/%s/issues/%d",
		url.PathEscape(owner), url.PathEscape(repo), number)
	var out Issue
	if err := c.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return Issue{}, err
	}
	return out, nil
}

// CreateIssueOptions describes a new issue.
type CreateIssueOptions struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

// CreateIssue opens a new issue in a repository.
func (c *Client) CreateIssue(ctx context.Context, owner, repo string, opts CreateIssueOptions) (Issue, error) {
	path := fmt.Sprintf("/repos/%s/%s/issues",
		url.PathEscape(owner), url.PathEscape(repo))
	var out Issue
	if err := c.do(ctx, http.MethodPost, path, opts, &out); err != nil {
		return Issue{}, err
	}
	return out, nil
}

// EditIssueOptions describes changes to an issue (close/reopen, rename).
type EditIssueOptions struct {
	State *string `json:"state,omitempty"` // "open" | "closed"
	Title *string `json:"title,omitempty"`
}

// EditIssue patches a Forgejo issue (e.g. close or reopen it).
func (c *Client) EditIssue(ctx context.Context, owner, repo string, number int64, opts EditIssueOptions) (Issue, error) {
	path := fmt.Sprintf("/repos/%s/%s/issues/%d",
		url.PathEscape(owner), url.PathEscape(repo), number)
	var out Issue
	if err := c.do(ctx, http.MethodPatch, path, opts, &out); err != nil {
		return Issue{}, err
	}
	return out, nil
}

// CreateIssueCommentBody adds a comment to an issue. Named to avoid shadowing
// the pull-request CreateIssueComment in pulls.go (which uses sudo).
func (c *Client) CreateIssueCommentBody(ctx context.Context, owner, repo string, number int64, body string) (IssueComment, error) {
	path := fmt.Sprintf("/repos/%s/%s/issues/%d/comments",
		url.PathEscape(owner), url.PathEscape(repo), number)
	payload := map[string]string{"body": body}
	var out IssueComment
	if err := c.do(ctx, http.MethodPost, path, payload, &out); err != nil {
		return IssueComment{}, err
	}
	return out, nil
}
