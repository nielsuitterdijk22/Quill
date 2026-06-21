package server

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/nielsuitterdijk22/quill/internal/forgejo"
	"github.com/nielsuitterdijk22/quill/internal/httpx"
	"github.com/nielsuitterdijk22/quill/internal/notify"
	"github.com/nielsuitterdijk22/quill/internal/platform"
)

// This file handles the Forgejo issue tracker surface exposed through Quill.
// Issues are owned by Forgejo; Quill proxies create/list/close/comment through
// the admin client (the admin token has access to all repos).

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

// resolveIssueRepo resolves the project/repo and returns the Forgejo owner+name.
// Returns the repo and Forgejo coordinates, or writes an error response.
func (s *Server) resolveIssueRepo(w http.ResponseWriter, r *http.Request) (platform.Actor, string, string, bool) {
	actor, ok := actorFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return platform.Actor{}, "", "", false
	}
	slug := chi.URLParam(r, "slug")
	repoSlug := chi.URLParam(r, "repo")
	owner, name, err := s.platform.ResolveRepoCoords(r.Context(), actor, slug, repoSlug)
	if err != nil {
		s.writePlatformError(w, err, "could not resolve repository")
		return platform.Actor{}, "", "", false
	}
	return actor, owner, name, true
}

// handleListIssues returns a repository's issues filtered by state.
func (s *Server) handleListIssues(w http.ResponseWriter, r *http.Request) {
	_, owner, name, ok := s.resolveIssueRepo(w, r)
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
	issues, err := s.forgejo.ListIssues(r.Context(), owner, name, state, page)
	if err != nil {
		s.logger.Error("list issues failed", "error", err)
		httpx.Error(w, http.StatusBadGateway, "forgejo_error", "could not load issues")
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
	_, owner, name, ok := s.resolveIssueRepo(w, r)
	if !ok {
		return
	}
	number, err := strconv.ParseInt(chi.URLParam(r, "number"), 10, 64)
	if err != nil || number <= 0 {
		httpx.Error(w, http.StatusBadRequest, "invalid_input", "issue number must be a positive integer")
		return
	}
	issue, err := s.forgejo.GetIssue(r.Context(), owner, name, number)
	if err != nil {
		if forgejo.NotFound(err) {
			httpx.Error(w, http.StatusNotFound, "not_found", "issue not found")
			return
		}
		httpx.Error(w, http.StatusBadGateway, "forgejo_error", "could not load issue")
		return
	}
	comments, err := s.forgejo.ListIssueComments(r.Context(), owner, name, int(number))
	if err != nil {
		comments = nil
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
	_, owner, name, ok := s.resolveIssueRepo(w, r)
	if !ok {
		return
	}
	var req createIssueRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Title == "" {
		httpx.Error(w, http.StatusBadRequest, "invalid_input", "title is required")
		return
	}
	issue, err := s.forgejo.CreateIssue(r.Context(), owner, name, forgejo.CreateIssueOptions{
		Title: req.Title,
		Body:  req.Body,
	})
	if err != nil {
		s.logger.Error("create issue failed", "error", err)
		httpx.Error(w, http.StatusBadGateway, "forgejo_error", "could not create issue")
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
	_, owner, name, ok := s.resolveIssueRepo(w, r)
	if !ok {
		return
	}
	number, err := strconv.ParseInt(chi.URLParam(r, "number"), 10, 64)
	if err != nil || number <= 0 {
		httpx.Error(w, http.StatusBadRequest, "invalid_input", "issue number must be a positive integer")
		return
	}
	var req editIssueRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	opts := forgejo.EditIssueOptions{}
	if req.State != "" {
		s := req.State
		opts.State = &s
	}
	if req.Title != "" {
		t := req.Title
		opts.Title = &t
	}
	issue, err := s.forgejo.EditIssue(r.Context(), owner, name, number, opts)
	if err != nil {
		if forgejo.NotFound(err) {
			httpx.Error(w, http.StatusNotFound, "not_found", "issue not found")
			return
		}
		httpx.Error(w, http.StatusBadGateway, "forgejo_error", "could not update issue")
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"issue": newIssueResponse(issue)})
}

type createIssueCommentRequest struct {
	Body string `json:"body"`
}

// handleCreateIssueComment adds a comment to an issue.
func (s *Server) handleCreateIssueComment(w http.ResponseWriter, r *http.Request) {
	_, owner, name, ok := s.resolveIssueRepo(w, r)
	if !ok {
		return
	}
	id, _ := identityFrom(r.Context())
	number, err := strconv.ParseInt(chi.URLParam(r, "number"), 10, 64)
	if err != nil || number <= 0 {
		httpx.Error(w, http.StatusBadRequest, "invalid_input", "issue number must be a positive integer")
		return
	}
	var req createIssueCommentRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Body == "" {
		httpx.Error(w, http.StatusBadRequest, "invalid_input", "comment body is required")
		return
	}
	comment, err := s.forgejo.CreateIssueCommentBody(r.Context(), owner, name, number, req.Body)
	if err != nil {
		s.logger.Error("create issue comment failed", "error", err)
		httpx.Error(w, http.StatusBadGateway, "forgejo_error", "could not post comment")
		return
	}
	httpx.JSON(w, http.StatusCreated, map[string]any{"comment": newIssueCommentResponse(comment)})
	projectSlug := chi.URLParam(r, "slug")
	repoSlug := chi.URLParam(r, "repo")
	s.notifier.NotifyMentions(r.Context(), notify.ParseMentions(req.Body), notify.MentionEvent{
		ProjectSlug:   projectSlug,
		RepoSlug:      repoSlug,
		Context:       "issue comment",
		ContextTitle:  owner + "/" + name + " #" + chi.URLParam(r, "number"),
		MentionerName: id.Username,
		BodyExcerpt:   truncate(req.Body, 300),
	})
}
