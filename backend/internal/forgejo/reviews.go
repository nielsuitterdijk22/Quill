package forgejo

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// This file wraps Forgejo's pull-request review surface: listing reviews and
// submitting them (approve, request changes, or comment). Reviews are the
// signal Quill's branch policies enforce when gating a merge. Submitting a
// review is attributed to the acting Quill user via sudo, like other writes.

// Review event states.
const (
	ReviewApproved       = "APPROVED"
	ReviewRequestChanges = "REQUEST_CHANGES"
	ReviewComment        = "COMMENT"
)

// Review is a pull-request review.
type Review struct {
	ID          int64     `json:"id"`
	User        *User     `json:"user"`
	State       string    `json:"state"` // APPROVED | REQUEST_CHANGES | COMMENT | PENDING
	Body        string    `json:"body"`
	CommitID    string    `json:"commit_id"`
	Stale       bool      `json:"stale"`
	Official    bool      `json:"official"`
	Dismissed   bool      `json:"dismissed"`
	SubmittedAt time.Time `json:"submitted_at"`
}

// CreateReviewOptions describes a review submission. Event is one of APPROVED,
// REQUEST_CHANGES, or COMMENT. Comments, when present, are line-anchored code
// comments attached to the review (used for diff line comments).
type CreateReviewOptions struct {
	Event    string               `json:"event"`
	Body     string               `json:"body,omitempty"`
	Comments []ReviewCommentInput `json:"comments,omitempty"`
}

// ReviewCommentInput anchors a review comment to a line in a file's diff.
// NewPosition is the line number in the new version of the file; OldPosition the
// line in the old version. One is set depending on the side being commented on.
type ReviewCommentInput struct {
	Path        string `json:"path"`
	Body        string `json:"body"`
	NewPosition int    `json:"new_position,omitempty"`
	OldPosition int    `json:"old_position,omitempty"`
}

// ReviewCodeComment is a single line-anchored comment within a review. Position
// is the line number in the new version of the file the comment is attached to.
type ReviewCodeComment struct {
	ID        int64     `json:"id"`
	Path      string    `json:"path"`
	Body      string    `json:"body"`
	Position  int       `json:"position"`
	User      *User     `json:"user"`
	CreatedAt time.Time `json:"created_at"`
	HTMLURL   string    `json:"html_url"`
}

// ListReviews returns the reviews submitted on a pull request.
func (c *Client) ListReviews(ctx context.Context, owner, repo string, number int) ([]Review, error) {
	var out []Review
	p := "/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(repo) + "/pulls/" + strconv.Itoa(number) + "/reviews"
	err := c.do(ctx, http.MethodGet, p, nil, &out)
	return out, err
}

// CreateReview submits a review on a pull request, attributed to asUser when
// non-empty.
func (c *Client) CreateReview(ctx context.Context, owner, repo string, number int, asUser string, opts CreateReviewOptions) (Review, error) {
	var out Review
	p := "/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(repo) + "/pulls/" + strconv.Itoa(number) + "/reviews" + sudoQuery(asUser)
	err := c.do(ctx, http.MethodPost, p, opts, &out)
	return out, err
}

// ListReviewComments returns the line-anchored comments attached to a single
// review on a pull request.
func (c *Client) ListReviewComments(ctx context.Context, owner, repo string, number int, reviewID int64) ([]ReviewCodeComment, error) {
	var out []ReviewCodeComment
	p := "/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(repo) +
		"/pulls/" + strconv.Itoa(number) + "/reviews/" + strconv.FormatInt(reviewID, 10) + "/comments"
	err := c.do(ctx, http.MethodGet, p, nil, &out)
	return out, err
}
