package platform

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/nielsuitterdijk22/quill/internal/forgejo"
	"github.com/nielsuitterdijk22/quill/internal/store"
)

// Issue tracker (D2 cutover). Engine-backed repos keep issues in Postgres
// (store/issues.go); Forgejo repos still proxy to Forgejo until migrated. Both
// paths return the forgejo DTOs the server already maps, so the frontend is
// unchanged. Authorization is project membership (resolvePullRepo), as before.

// issuePageSize mirrors Forgejo's default page.
const issuePageSize = 30

func issueUser(name string) *forgejo.User {
	return &forgejo.User{Login: name, FullName: name, Type: "User"}
}

func issueDTO(is store.Issue) forgejo.Issue {
	return forgejo.Issue{
		Number: int64(is.Number), Title: is.Title, Body: is.Body, State: is.State,
		User: issueUser(is.CreatedByName), Labels: []forgejo.Label{},
		Comments: is.Comments, CreatedAt: is.CreatedAt, UpdatedAt: is.UpdatedAt,
	}
}

func issueCommentDTO(c store.IssueThreadComment) forgejo.IssueComment {
	return forgejo.IssueComment{
		ID: c.ID, Body: c.Body, User: issueUser(c.AuthorName),
		CreatedAt: c.CreatedAt, UpdatedAt: c.CreatedAt,
	}
}

// actorLogin is the username snapshot stored on rows the actor authors.
func (s *Service) actorLogin(ctx context.Context, actor Actor) (string, error) {
	u, err := s.store.GetUserByID(ctx, actor.UserID)
	if err != nil {
		return "", fmt.Errorf("load acting user: %w", err)
	}
	return u.Username, nil
}

func (s *Service) engineIssue(ctx context.Context, be gitBackend, number int) (store.Issue, error) {
	is, err := s.store.GetIssueByNumber(ctx, be.repoID, number)
	if errors.Is(err, pgx.ErrNoRows) {
		return store.Issue{}, ErrNotFound
	}
	return is, err
}

// ListIssues pages a repository's issues by state ("open" | "closed" | "all").
func (s *Service) ListIssues(ctx context.Context, actor Actor, projectSlug, repoSlug, state string, page int) ([]forgejo.Issue, error) {
	_, be, err := s.resolvePullRepo(ctx, actor, projectSlug, repoSlug)
	if err != nil {
		return nil, err
	}
	if !be.engine {
		issues, err := s.forgejo.ListIssues(ctx, be.owner, be.name, state, page)
		if err != nil {
			return nil, translateForgejoRead(err)
		}
		return issues, nil
	}
	rows, err := s.store.ListIssuesByRepo(ctx, be.repoID, state, page, issuePageSize)
	if err != nil {
		return nil, err
	}
	out := make([]forgejo.Issue, 0, len(rows))
	for _, is := range rows {
		out = append(out, issueDTO(is))
	}
	return out, nil
}

// GetIssue returns one issue with its comments.
func (s *Service) GetIssue(ctx context.Context, actor Actor, projectSlug, repoSlug string, number int) (forgejo.Issue, []forgejo.IssueComment, error) {
	_, be, err := s.resolvePullRepo(ctx, actor, projectSlug, repoSlug)
	if err != nil {
		return forgejo.Issue{}, nil, err
	}
	if !be.engine {
		issue, err := s.forgejo.GetIssue(ctx, be.owner, be.name, int64(number))
		if err != nil {
			return forgejo.Issue{}, nil, translateForgejoRead(err)
		}
		comments, err := s.forgejo.ListIssueComments(ctx, be.owner, be.name, number)
		if err != nil {
			return forgejo.Issue{}, nil, translateForgejoRead(err)
		}
		return issue, comments, nil
	}
	is, err := s.engineIssue(ctx, be, number)
	if err != nil {
		return forgejo.Issue{}, nil, err
	}
	rows, err := s.store.ListIssueThreadComments(ctx, is.ID)
	if err != nil {
		return forgejo.Issue{}, nil, err
	}
	comments := make([]forgejo.IssueComment, 0, len(rows))
	for _, c := range rows {
		comments = append(comments, issueCommentDTO(c))
	}
	return issueDTO(is), comments, nil
}

// CreateIssue opens an issue attributed to the actor.
func (s *Service) CreateIssue(ctx context.Context, actor Actor, projectSlug, repoSlug, title, body string) (forgejo.Issue, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return forgejo.Issue{}, fmt.Errorf("%w: a title is required", ErrInvalidInput)
	}
	_, be, err := s.resolvePullRepo(ctx, actor, projectSlug, repoSlug)
	if err != nil {
		return forgejo.Issue{}, err
	}
	if !be.engine {
		issue, err := s.forgejo.CreateIssue(ctx, be.owner, be.name, forgejo.CreateIssueOptions{Title: title, Body: body})
		if err != nil {
			return forgejo.Issue{}, translateForgejoWrite(err)
		}
		return issue, nil
	}
	login, err := s.actorLogin(ctx, actor)
	if err != nil {
		return forgejo.Issue{}, err
	}
	uid := actor.UserID
	is, err := s.store.CreateIssue(ctx, be.repoID, be.tenantID, &uid, login, title, body)
	if err != nil {
		return forgejo.Issue{}, err
	}
	return issueDTO(is), nil
}

// EditIssue changes an issue's state ("open" | "closed") and/or title.
func (s *Service) EditIssue(ctx context.Context, actor Actor, projectSlug, repoSlug string, number int, state, title *string) (forgejo.Issue, error) {
	if state != nil && *state != "open" && *state != "closed" {
		return forgejo.Issue{}, fmt.Errorf("%w: state must be open or closed", ErrInvalidInput)
	}
	if title != nil {
		t := strings.TrimSpace(*title)
		if t == "" {
			return forgejo.Issue{}, fmt.Errorf("%w: a title is required", ErrInvalidInput)
		}
		title = &t
	}
	_, be, err := s.resolvePullRepo(ctx, actor, projectSlug, repoSlug)
	if err != nil {
		return forgejo.Issue{}, err
	}
	if !be.engine {
		issue, err := s.forgejo.EditIssue(ctx, be.owner, be.name, int64(number), forgejo.EditIssueOptions{State: state, Title: title})
		if err != nil {
			return forgejo.Issue{}, translateForgejoWrite(err)
		}
		return issue, nil
	}
	is, err := s.engineIssue(ctx, be, number)
	if err != nil {
		return forgejo.Issue{}, err
	}
	is, err = s.store.UpdateIssue(ctx, is.ID, title, state)
	if err != nil {
		return forgejo.Issue{}, err
	}
	return issueDTO(is), nil
}

// CreateIssueComment adds a comment to an issue, attributed to the actor.
func (s *Service) CreateIssueComment(ctx context.Context, actor Actor, projectSlug, repoSlug string, number int, body string) (forgejo.IssueComment, error) {
	body = strings.TrimSpace(body)
	if body == "" {
		return forgejo.IssueComment{}, fmt.Errorf("%w: a comment body is required", ErrInvalidInput)
	}
	_, be, err := s.resolvePullRepo(ctx, actor, projectSlug, repoSlug)
	if err != nil {
		return forgejo.IssueComment{}, err
	}
	if !be.engine {
		c, err := s.forgejo.CreateIssueCommentBody(ctx, be.owner, be.name, int64(number), body)
		if err != nil {
			return forgejo.IssueComment{}, translateForgejoWrite(err)
		}
		return c, nil
	}
	is, err := s.engineIssue(ctx, be, number)
	if err != nil {
		return forgejo.IssueComment{}, err
	}
	login, err := s.actorLogin(ctx, actor)
	if err != nil {
		return forgejo.IssueComment{}, err
	}
	uid := actor.UserID
	c, err := s.store.CreateIssueThreadComment(ctx, is.ID, be.tenantID, &uid, login, body)
	if err != nil {
		return forgejo.IssueComment{}, err
	}
	return issueCommentDTO(c), nil
}
