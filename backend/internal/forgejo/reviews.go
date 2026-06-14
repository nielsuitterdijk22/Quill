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
// REQUEST_CHANGES, or COMMENT.
type CreateReviewOptions struct {
	Event string `json:"event"`
	Body  string `json:"body,omitempty"`
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
