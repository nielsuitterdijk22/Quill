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

// This file handles the issue tracker surface. The platform layer owns the
// backend choice: engine-backed repos keep issues in Postgres (D2 cutover),
// Forgejo repos still proxy to Forgejo until migrated.

// issueResponse is the public JSON shape for a Forgejo issue.
type issueResponse struct {
	Number    int64            `json:"number"`
	Title     string           `json:"title"`
	Body      string           `json:"body"`
	State     string           `json:"state"`
	Author    *userRef         `json:"author,omitempty"`
	Comments  int              `json:"comments"`
	CreatedAt time.Time        `json:"createdAt"`
	UpdatedAt time.Time        `json:"updatedAt"`
	Labels    []issueLabelResp `json:"labels"`
}

type issueLabelResp struct {
	Name  string `json:"name"`
	Color string `json:"color"`
}

func newIssueResponse(i forgejo.Issue) issueResponse {
	labels := make([]issueLabelResp, len(i.Labels))
	for j, l := range i.Labels {
		labels[j] = issueLabelResp{Name: l.Name, Color: l.Color}
	}
	return issueResponse{
		Number:    i.Number,
		Title:     i.Title,
		Body:      i.Body,
		State:     i.State,
		Author:    newUserRef(i.User),
		Comments:  i.Comments,
		CreatedAt: i.CreatedAt,
		UpdatedAt: i.UpdatedAt,
		Labels:    labels,
	}
}

// issueCommentResponse is the public JSON shape for an issue comment.
type issueCommentResponse struct {
	ID        int64     `json:"id"`
	Body      string    `json:"body"`
	Author    *userRef  `json:"author,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

func newIssueCommentResponse(c forgejo.IssueComment) issueCommentResponse {
	return issueCommentResponse{
		ID:        c.ID,
		Body:      c.Body,
		Author:    newUserRef(c.User),
		CreatedAt: c.CreatedAt,
	}
}

// issueTarget extracts the actor + project/repo slugs, or writes 401.
func issueTarget(w http.ResponseWriter, r *http.Request) (platform.Actor, string, string, bool) {
	actor, ok := actorFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return platform.Actor{}, "", "", false
	}
	return actor, chi.URLParam(r, "slug"), chi.URLParam(r, "repo"), true
}

// issueNumber parses the {number} path param, or writes 400.
func issueNumber(w http.ResponseWriter, r *http.Request) (int, bool) {
	number, err := strconv.Atoi(chi.URLParam(r, "number"))
	if err != nil || number <= 0 {
		httpx.Error(w, http.StatusBadRequest, "invalid_input", "issue number must be a positive integer")
		return 0, false
	}
	return number, true
}

// handleListIssues returns a repository's issues filtered by state.
func (s *Server) handleListIssues(w http.ResponseWriter, r *http.Request) {
	actor, slug, repo, ok := issueTarget(w, r)
	if !ok {
		return
	}
	state := r.URL.Query().Get("state")
	if state == "" {
		state = "open"
	}
	page := 1
	if p := r.URL.Query().Get("page"); p != "" {
		if n, err := strconv.Atoi(p); err == nil && n > 0 {
			page = n
		}
	}
	issues, err := s.platform.ListIssues(r.Context(), actor, slug, repo, state, page)
	if err != nil {
		s.writePlatformError(w, err, "could not load issues")
		return
	}
	out := make([]issueResponse, len(issues))
	for i, iss := range issues {
		out[i] = newIssueResponse(iss)
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"issues": out})
}

// handleGetIssue returns a single issue with its comments.
func (s *Server) handleGetIssue(w http.ResponseWriter, r *http.Request) {
	actor, slug, repo, ok := issueTarget(w, r)
	if !ok {
		return
	}
	number, ok := issueNumber(w, r)
	if !ok {
		return
	}
	issue, comments, err := s.platform.GetIssue(r.Context(), actor, slug, repo, number)
	if err != nil {
		s.writePlatformError(w, err, "could not load issue")
		return
	}
	out := make([]issueCommentResponse, len(comments))
	for i, c := range comments {
		out[i] = newIssueCommentResponse(c)
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"issue":    newIssueResponse(issue),
		"comments": out,
	})
}

type createIssueRequest struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

// handleCreateIssue opens a new issue in the repository.
func (s *Server) handleCreateIssue(w http.ResponseWriter, r *http.Request) {
	actor, slug, repo, ok := issueTarget(w, r)
	if !ok {
		return
	}
	var req createIssueRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	issue, err := s.platform.CreateIssue(r.Context(), actor, slug, repo, req.Title, req.Body)
	if err != nil {
		s.writePlatformError(w, err, "could not create issue")
		return
	}
	httpx.JSON(w, http.StatusCreated, map[string]any{"issue": newIssueResponse(issue)})
}

type editIssueRequest struct {
	State string `json:"state"` // "open" | "closed"
	Title string `json:"title"`
}

// handleEditIssue closes, reopens, or renames an issue.
func (s *Server) handleEditIssue(w http.ResponseWriter, r *http.Request) {
	actor, slug, repo, ok := issueTarget(w, r)
	if !ok {
		return
	}
	number, ok := issueNumber(w, r)
	if !ok {
		return
	}
	var req editIssueRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	var state, title *string
	if req.State != "" {
		state = &req.State
	}
	if req.Title != "" {
		title = &req.Title
	}
	issue, err := s.platform.EditIssue(r.Context(), actor, slug, repo, number, state, title)
	if err != nil {
		s.writePlatformError(w, err, "could not update issue")
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"issue": newIssueResponse(issue)})
}

type createIssueCommentRequest struct {
	Body string `json:"body"`
}

// handleCreateIssueComment adds a comment to an issue.
func (s *Server) handleCreateIssueComment(w http.ResponseWriter, r *http.Request) {
	actor, slug, repo, ok := issueTarget(w, r)
	if !ok {
		return
	}
	number, ok := issueNumber(w, r)
	if !ok {
		return
	}
	var req createIssueCommentRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	comment, err := s.platform.CreateIssueComment(r.Context(), actor, slug, repo, number, req.Body)
	if err != nil {
		s.writePlatformError(w, err, "could not post comment")
		return
	}
	httpx.JSON(w, http.StatusCreated, map[string]any{"comment": newIssueCommentResponse(comment)})
}
