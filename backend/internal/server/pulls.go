package server

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/nielsuitterdijk22/quill/internal/forgejo"
	"github.com/nielsuitterdijk22/quill/internal/httpx"
	"github.com/nielsuitterdijk22/quill/internal/platform"
)

// This file holds the pull-request endpoints added in PR 6: listing, viewing,
// creating, commenting on, diffing, and merging pull requests. They translate
// the platform service's Forgejo-backed results into clean JSON for the frontend.

// userRef is the public shape for a git/forge user reference.
type userRef struct {
	Login string `json:"login"`
	Name  string `json:"name,omitempty"`
}

func newUserRef(u *forgejo.User) *userRef {
	if u == nil {
		return nil
	}
	return &userRef{Login: u.Login, Name: u.FullName}
}

// prRef is one side (head/base) of a pull request.
type prRef struct {
	Label string `json:"label"`
	Ref   string `json:"ref"`
	SHA   string `json:"sha"`
}

// pullResponse is the public JSON shape for a pull request.
type pullResponse struct {
	Number       int        `json:"number"`
	Title        string     `json:"title"`
	Body         string     `json:"body"`
	State        string     `json:"state"`
	Draft        bool       `json:"draft"`
	Merged       bool       `json:"merged"`
	Mergeable    bool       `json:"mergeable"`
	Comments     int        `json:"comments"`
	Additions    int        `json:"additions"`
	Deletions    int        `json:"deletions"`
	ChangedFiles int        `json:"changedFiles"`
	Author       *userRef   `json:"author"`
	Head         prRef      `json:"head"`
	Base         prRef      `json:"base"`
	HTMLURL      string     `json:"htmlUrl"`
	CreatedAt    time.Time  `json:"createdAt"`
	UpdatedAt    time.Time  `json:"updatedAt"`
	MergedAt     *time.Time `json:"mergedAt,omitempty"`
	MergedBy     *userRef   `json:"mergedBy,omitempty"`
	MergeCommit  string     `json:"mergeCommitSha,omitempty"`
}

func newPullResponse(p forgejo.PullRequest) pullResponse {
	return pullResponse{
		Number:       p.Number,
		Title:        p.Title,
		Body:         p.Body,
		State:        p.State,
		Draft:        p.Draft,
		Merged:       p.Merged,
		Mergeable:    p.Mergeable,
		Comments:     p.Comments,
		Additions:    p.Additions,
		Deletions:    p.Deletions,
		ChangedFiles: p.ChangedFiles,
		Author:       newUserRef(p.User),
		Head:         prRef{Label: p.Head.Label, Ref: p.Head.Ref, SHA: p.Head.SHA},
		Base:         prRef{Label: p.Base.Label, Ref: p.Base.Ref, SHA: p.Base.SHA},
		HTMLURL:      p.HTMLURL,
		CreatedAt:    p.CreatedAt,
		UpdatedAt:    p.UpdatedAt,
		MergedAt:     p.MergedAt,
		MergedBy:     newUserRef(p.MergedBy),
		MergeCommit:  p.MergeCommit,
	}
}

// pullCommentResponse is the public JSON shape for a PR conversation comment.
type pullCommentResponse struct {
	ID        int64     `json:"id"`
	Body      string    `json:"body"`
	Author    *userRef  `json:"author"`
	CreatedAt time.Time `json:"createdAt"`
}

func newPullCommentResponse(c forgejo.IssueComment) pullCommentResponse {
	return pullCommentResponse{
		ID:        c.ID,
		Body:      c.Body,
		Author:    newUserRef(c.User),
		CreatedAt: c.CreatedAt,
	}
}

// diffLineResponse / diffHunkResponse / diffFileResponse describe a parsed diff.
type diffLineResponse struct {
	Type      string `json:"type"` // context | add | del
	Content   string `json:"content"`
	OldNumber int    `json:"oldNumber"`
	NewNumber int    `json:"newNumber"`
}

type diffHunkResponse struct {
	Header string             `json:"header"`
	Lines  []diffLineResponse `json:"lines"`
}

type diffFileResponse struct {
	Path      string             `json:"path"`
	OldPath   string             `json:"oldPath"`
	Status    string             `json:"status"`
	IsBinary  bool               `json:"isBinary"`
	Additions int                `json:"additions"`
	Deletions int                `json:"deletions"`
	Hunks     []diffHunkResponse `json:"hunks"`
}

func newDiffFiles(files []forgejo.DiffFile) []diffFileResponse {
	out := make([]diffFileResponse, 0, len(files))
	for _, f := range files {
		hunks := make([]diffHunkResponse, 0, len(f.Hunks))
		for _, h := range f.Hunks {
			lines := make([]diffLineResponse, 0, len(h.Lines))
			for _, l := range h.Lines {
				lines = append(lines, diffLineResponse{
					Type:      l.Type,
					Content:   l.Content,
					OldNumber: l.OldNumber,
					NewNumber: l.NewNumber,
				})
			}
			hunks = append(hunks, diffHunkResponse{Header: h.Header, Lines: lines})
		}
		out = append(out, diffFileResponse{
			Path:      f.Path,
			OldPath:   f.OldPath,
			Status:    f.Status,
			IsBinary:  f.IsBinary,
			Additions: f.Additions,
			Deletions: f.Deletions,
			Hunks:     hunks,
		})
	}
	return out
}

type createPullRequest struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	Head  string `json:"head"`
	Base  string `json:"base"`
}

type createCommentRequest struct {
	Body string `json:"body"`
}

type mergePullRequest struct {
	Method string `json:"method"`
}

// handleListPulls returns a repository's pull requests.
func (s *Server) handleListPulls(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	state := r.URL.Query().Get("state")
	repo, pulls, err := s.platform.ListPulls(r.Context(), actor, chi.URLParam(r, "slug"), chi.URLParam(r, "repo"), state)
	if err != nil {
		s.writePlatformError(w, err, "could not list pull requests")
		return
	}
	out := make([]pullResponse, 0, len(pulls))
	for _, p := range pulls {
		out = append(out, newPullResponse(p))
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"repository": newRepoResponse(repo),
		"pulls":      out,
	})
}

// handleGetPull returns a single pull request.
func (s *Server) handleGetPull(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	number, ok := pullNumber(w, r)
	if !ok {
		return
	}
	repo, pr, err := s.platform.GetPull(r.Context(), actor, chi.URLParam(r, "slug"), chi.URLParam(r, "repo"), number)
	if err != nil {
		s.writePlatformError(w, err, "could not load pull request")
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"repository": newRepoResponse(repo),
		"pull":       newPullResponse(pr),
	})
}

// handleGetPullDiff returns a pull request's parsed diff.
func (s *Server) handleGetPullDiff(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	number, ok := pullNumber(w, r)
	if !ok {
		return
	}
	_, files, err := s.platform.GetPullDiff(r.Context(), actor, chi.URLParam(r, "slug"), chi.URLParam(r, "repo"), number)
	if err != nil {
		s.writePlatformError(w, err, "could not load diff")
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"files": newDiffFiles(files)})
}

// handleListPullComments returns a pull request's conversation comments.
func (s *Server) handleListPullComments(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	number, ok := pullNumber(w, r)
	if !ok {
		return
	}
	comments, err := s.platform.ListPullComments(r.Context(), actor, chi.URLParam(r, "slug"), chi.URLParam(r, "repo"), number)
	if err != nil {
		s.writePlatformError(w, err, "could not list comments")
		return
	}
	out := make([]pullCommentResponse, 0, len(comments))
	for _, c := range comments {
		out = append(out, newPullCommentResponse(c))
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"comments": out})
}

// handleCreatePull opens a new pull request.
func (s *Server) handleCreatePull(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	var req createPullRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	_, pr, err := s.platform.CreatePull(r.Context(), actor, chi.URLParam(r, "slug"), chi.URLParam(r, "repo"), platform.CreatePullInput{
		Title: req.Title,
		Body:  req.Body,
		Head:  req.Head,
		Base:  req.Base,
	})
	if err != nil {
		s.writePlatformError(w, err, "could not create pull request")
		return
	}
	httpx.JSON(w, http.StatusCreated, map[string]any{"pull": newPullResponse(pr)})
}

// handleCreatePullComment adds a comment to a pull request.
func (s *Server) handleCreatePullComment(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	number, ok := pullNumber(w, r)
	if !ok {
		return
	}
	var req createCommentRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	comment, err := s.platform.CreatePullComment(r.Context(), actor, chi.URLParam(r, "slug"), chi.URLParam(r, "repo"), number, req.Body)
	if err != nil {
		s.writePlatformError(w, err, "could not add comment")
		return
	}
	httpx.JSON(w, http.StatusCreated, map[string]any{"comment": newPullCommentResponse(comment)})
}

// handleMergePull merges a pull request.
func (s *Server) handleMergePull(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	number, ok := pullNumber(w, r)
	if !ok {
		return
	}
	var req mergePullRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	_, pr, err := s.platform.MergePull(r.Context(), actor, chi.URLParam(r, "slug"), chi.URLParam(r, "repo"), number, req.Method)
	if err != nil {
		s.writePlatformError(w, err, "could not merge pull request")
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"pull": newPullResponse(pr)})
}

// pullNumber parses the {number} URL parameter, writing a 400 and returning
// false when it is not a positive integer.
func pullNumber(w http.ResponseWriter, r *http.Request) (int, bool) {
	n, err := strconv.Atoi(chi.URLParam(r, "number"))
	if err != nil || n <= 0 {
		httpx.Error(w, http.StatusBadRequest, "invalid_input", "pull request number must be a positive integer")
		return 0, false
	}
	return n, true
}

// reviewResponse is the public JSON shape for a pull-request review.
type reviewResponse struct {
	ID          int64     `json:"id"`
	State       string    `json:"state"`
	Body        string    `json:"body"`
	Author      *userRef  `json:"author"`
	Stale       bool      `json:"stale"`
	Dismissed   bool      `json:"dismissed"`
	SubmittedAt time.Time `json:"submittedAt"`
}

func newReviewResponse(rv forgejo.Review) reviewResponse {
	return reviewResponse{
		ID:          rv.ID,
		State:       rv.State,
		Body:        rv.Body,
		Author:      newUserRef(rv.User),
		Stale:       rv.Stale,
		Dismissed:   rv.Dismissed,
		SubmittedAt: rv.SubmittedAt,
	}
}

// policyGateResponse describes whether a branch policy permits merging a PR.
type policyGateResponse struct {
	Applies           bool   `json:"applies"`
	Pattern           string `json:"pattern,omitempty"`
	RequiredApprovals int    `json:"requiredApprovals"`
	Approvals         int    `json:"approvals"`
	ChangesRequested  int    `json:"changesRequested"`
	Blocked           bool   `json:"blocked"`
	Reason            string `json:"reason,omitempty"`
}

func newPolicyGate(state platform.ReviewState) policyGateResponse {
	gate := policyGateResponse{
		Approvals:        state.Approvals,
		ChangesRequested: state.ChangesRequested,
		Blocked:          state.Blocked,
		Reason:           state.Reason,
	}
	if state.Policy != nil {
		gate.Applies = true
		gate.Pattern = state.Policy.Pattern
		gate.RequiredApprovals = int(state.Policy.RequiredApprovals)
	}
	return gate
}

type createReviewRequest struct {
	Event string `json:"event"`
	Body  string `json:"body"`
}

// handleListPullReviews returns a pull request's reviews and the policy gate
// evaluated against its base branch.
func (s *Server) handleListPullReviews(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	number, ok := pullNumber(w, r)
	if !ok {
		return
	}
	reviews, state, err := s.platform.ReviewsAndState(r.Context(), actor, chi.URLParam(r, "slug"), chi.URLParam(r, "repo"), number)
	if err != nil {
		s.writePlatformError(w, err, "could not list reviews")
		return
	}
	out := make([]reviewResponse, 0, len(reviews))
	for _, rv := range reviews {
		out = append(out, newReviewResponse(rv))
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"reviews": out,
		"gate":    newPolicyGate(state),
	})
}

// handleCreatePullReview submits a review on a pull request.
func (s *Server) handleCreatePullReview(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	number, ok := pullNumber(w, r)
	if !ok {
		return
	}
	var req createReviewRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	review, err := s.platform.CreatePullReview(r.Context(), actor, chi.URLParam(r, "slug"), chi.URLParam(r, "repo"), number, req.Event, req.Body)
	if err != nil {
		s.writePlatformError(w, err, "could not submit review")
		return
	}
	httpx.JSON(w, http.StatusCreated, map[string]any{"review": newReviewResponse(review)})
}
