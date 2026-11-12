package platform

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/google/uuid"

	"github.com/nielsuitterdijk22/quill/internal/forgejo"
	"github.com/nielsuitterdijk22/quill/internal/pipeline"
	"github.com/nielsuitterdijk22/quill/internal/policy"
	"github.com/nielsuitterdijk22/quill/internal/store/db"
)

// This file exposes pull-request operations on top of Forgejo. Reads use the
// admin token (Quill enforces visibility via project membership, exactly like code
// browsing). Writes that create user-visible content (opening a PR, commenting,
// merging) are attributed to the acting Quill user via Forgejo's sudo facility;
// to make that attribution work the user is first ensured as a Forgejo
// collaborator on the repository.

// CreatePullInput is the payload for opening a pull request.
type CreatePullInput struct {
	Title string
	Body  string
	Head  string
	Base  string
}

// validMergeMethods are the merge strategies Quill accepts.
var validMergeMethods = map[string]bool{"merge": true, "squash": true, "rebase": true}

// validReviewEvents are the review actions Quill accepts.
var validReviewEvents = map[string]bool{
	forgejo.ReviewApproved:       true,
	forgejo.ReviewRequestChanges: true,
	forgejo.ReviewComment:        true,
}

// ListPulls returns a repository's pull requests filtered by state ("open",
// "closed", or "all").
func (s *Service) ListPulls(ctx context.Context, actor Actor, projectSlug, repoSlug, state string) (db.Repository, []forgejo.PullRequest, error) {
	repo, be, err := s.resolvePullRepo(ctx, actor, projectSlug, repoSlug)
	if err != nil {
		return db.Repository{}, nil, err
	}
	if be.engine {
		pulls, err := s.engineListPulls(ctx, repo, be, state)
		return repo, pulls, err
	}
	pulls, err := s.forgejo.ListPulls(ctx, be.owner, be.name, state, 0)
	if err != nil {
		return db.Repository{}, nil, translateForgejoRead(err)
	}
	return repo, pulls, nil
}

// maxPullCountConcurrency bounds how many repositories Quill queries Forgejo for
// open-PR counts at once when computing the dashboard aggregate.
const maxPullCountConcurrency = 8

// OpenPullRequestCount returns the total number of open pull requests across
// every repository in the projects the actor belongs to. Counts come from
// Forgejo's exact total (not a capped page), and repositories are queried
// concurrently with a bounded worker pool. It is best-effort: a repository whose
// count can't be read is treated as zero and logged, mirroring the dashboard's
// tolerance for partial data.
func (s *Service) OpenPullRequestCount(ctx context.Context, actor Actor) (int, error) {
	if !s.forgejoEnabled() && s.gitEngine == nil {
		return 0, nil
	}

	projects, err := s.visibleProjects(ctx, actor, "")
	if err != nil {
		return 0, err
	}

	// Engine-backed repos contribute straight from Postgres.
	engineTotal := 0
	if s.gitEngine != nil {
		ids := make([]uuid.UUID, 0, len(projects))
		for _, p := range projects {
			ids = append(ids, p.ID)
		}
		n, err := s.store.CountEngineOpenPulls(ctx, ids)
		if err != nil {
			return 0, err
		}
		engineTotal = n
	}
	if !s.forgejoEnabled() {
		return engineTotal, nil
	}

	type target struct{ owner, name string }
	var targets []target
	for _, project := range projects {
		repos, err := s.store.ListRepositoriesByProject(ctx, db.ListRepositoriesByProjectParams{ProjectID: project.ID, Limit: 200, Offset: 0})
		if err != nil {
			return 0, fmt.Errorf("list repos: %w", err)
		}
		for _, repo := range repos {
			if owner, name, ok := forgejoTarget(repo, project); ok {
				targets = append(targets, target{owner: owner, name: name})
			}
		}
	}
	if len(targets) == 0 {
		return 0, nil
	}

	var total int64
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(maxPullCountConcurrency)
	for _, t := range targets {
		t := t
		g.Go(func() error {
			n, err := s.forgejo.CountOpenPulls(gctx, t.owner, t.name)
			if err != nil {
				// Best-effort: an empty repo or transient failure contributes zero.
				s.logger.Warn("open pull-request count failed", "repo", t.owner+"/"+t.name, "error", err)
				return nil
			}
			atomic.AddInt64(&total, int64(n))
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return 0, err
	}
	return int(total) + engineTotal, nil
}

// RepoPull pairs a pull request with the repository and project it belongs
// to, so a cross-repository listing carries the context the frontend needs to
// link back to each PR.
type RepoPull struct {
	ProjectSlug string
	RepoSlug    string
	RepoName    string
	Pull        forgejo.PullRequest
}

// ListOpenPullsInput holds the optional cheap filters for ListOpenPulls.
type ListOpenPullsInput struct {
	// State is "open" (default), "closed", or "all".
	State string
	// ProjectSlug, when non-empty, restricts the listing to a single project.
	ProjectSlug string
}

// maxPullListConcurrency bounds how many repositories Quill queries Forgejo for
// pull requests at once when building the cross-repository overview.
const maxPullListConcurrency = 8

// maxPullsPerRepo caps how many pull requests Quill pulls from any one
// repository for the overview, keeping a single noisy repo from dominating the
// aggregate (and the response size).
const maxPullsPerRepo = 50

// visibleProjects returns the projects that scope the actor's cross-cutting
// user views (PRs overview, dashboard counts): those reached through a direct
// project_members row or the actor's org-tenant memberships. A non-empty
// projectSlug narrows to that project without granting access to it — naming a
// project you can't see yields nothing. Platform admins are deliberately NOT
// broadened: their user views show their own memberships (admin visibility
// belongs to admin tooling), which is what the unified visibility SQL in
// groups.sql (GetEffectiveProjectRole) does not model.
func (s *Service) visibleProjects(ctx context.Context, actor Actor, projectSlug string) ([]db.Project, error) {
	projects, err := s.store.ListVisibleProjectsForUser(ctx, actor.UserID)
	if err != nil {
		return nil, fmt.Errorf("list visible projects: %w", err)
	}
	if projectSlug == "" {
		return projects, nil
	}
	filtered := make([]db.Project, 0, 1)
	for _, p := range projects {
		if strings.EqualFold(p.Slug, projectSlug) {
			filtered = append(filtered, p)
		}
	}
	return filtered, nil
}

// ListOpenPulls returns pull requests across every repository in the
// projects the actor belongs to, newest-updated first. Like
// OpenPullRequestCount it is best-effort: a repository whose pulls can't be read
// is skipped and logged, mirroring the dashboard's tolerance for partial data.
// The optional input narrows the result by state and/or project.
func (s *Service) ListOpenPulls(ctx context.Context, actor Actor, in ListOpenPullsInput) ([]RepoPull, error) {
	if !s.forgejoEnabled() && s.gitEngine == nil {
		return nil, nil
	}
	state := in.State
	if state == "" {
		state = "open"
	}

	projects, err := s.visibleProjects(ctx, actor, in.ProjectSlug)
	if err != nil {
		return nil, err
	}

	// Engine-backed repos contribute straight from Postgres (mergeable is
	// computed live per visible repo; the overview's PR cards need it).
	var all []RepoPull
	if s.gitEngine != nil {
		ids := make([]uuid.UUID, 0, len(projects))
		for _, p := range projects {
			ids = append(ids, p.ID)
		}
		rows, err := s.store.ListEnginePullsByProjects(ctx, ids, state)
		if err != nil {
			return nil, err
		}
		for _, l := range rows {
			repo, err := s.store.GetRepositoryByID(ctx, l.Pull.RepoID)
			if err != nil {
				continue // best-effort, like the forgejo arm
			}
			project, err := s.store.GetProjectByID(ctx, repo.ProjectID)
			if err != nil {
				continue
			}
			be := gitBackend{engine: true, repoID: repo.ID, tenantID: project.TenantID,
				projectSlug: l.ProjectSlug, repoSlug: l.RepoSlug}
			if dir, err := s.engineDir(ctx, be); err == nil {
				if dto, err := s.enginePullDTO(ctx, repo, be, dir, l.Pull, true); err == nil {
					all = append(all, RepoPull{ProjectSlug: l.ProjectSlug, RepoSlug: l.RepoSlug,
						RepoName: l.RepoName, Pull: dto})
				}
			}
		}
	}
	if !s.forgejoEnabled() {
		sort.Slice(all, func(i, j int) bool { return all[i].Pull.UpdatedAt.After(all[j].Pull.UpdatedAt) })
		return all, nil
	}

	type target struct {
		owner, name, projectSlug, repoSlug, repoName string
	}
	var targets []target
	for _, project := range projects {
		repos, err := s.store.ListRepositoriesByProject(ctx, db.ListRepositoriesByProjectParams{ProjectID: project.ID, Limit: 200, Offset: 0})
		if err != nil {
			return nil, fmt.Errorf("list repos: %w", err)
		}
		for _, repo := range repos {
			if owner, name, ok := forgejoTarget(repo, project); ok {
				targets = append(targets, target{
					owner:       owner,
					name:        name,
					projectSlug: project.Slug,
					repoSlug:    repo.Slug,
					repoName:    repo.Name,
				})
			}
		}
	}
	if len(targets) == 0 {
		return nil, nil
	}

	results := make([][]RepoPull, len(targets))
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(maxPullListConcurrency)
	for i, t := range targets {
		i, t := i, t
		g.Go(func() error {
			pulls, err := s.forgejo.ListPulls(gctx, t.owner, t.name, state, maxPullsPerRepo)
			if err != nil {
				// Best-effort: an empty repo or transient failure contributes nothing.
				s.logger.Warn("cross-repo pull listing failed", "repo", t.owner+"/"+t.name, "error", err)
				return nil
			}
			out := make([]RepoPull, 0, len(pulls))
			for _, p := range pulls {
				out = append(out, RepoPull{ProjectSlug: t.projectSlug, RepoSlug: t.repoSlug, RepoName: t.repoName, Pull: p})
			}
			results[i] = out
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}

	for _, r := range results {
		all = append(all, r...)
	}
	// Newest-updated first across all repositories.
	sort.Slice(all, func(i, j int) bool { return all[i].Pull.UpdatedAt.After(all[j].Pull.UpdatedAt) })
	return all, nil
}

// GetPull returns a single pull request by number.
func (s *Service) GetPull(ctx context.Context, actor Actor, projectSlug, repoSlug string, number int) (db.Repository, forgejo.PullRequest, error) {
	repo, be, err := s.resolvePullRepo(ctx, actor, projectSlug, repoSlug)
	if err != nil {
		return db.Repository{}, forgejo.PullRequest{}, err
	}
	if be.engine {
		_, dto, err := s.engineGetPull(ctx, repo, be, number)
		return repo, dto, err
	}
	pr, err := s.forgejo.GetPull(ctx, be.owner, be.name, number)
	if err != nil {
		return db.Repository{}, forgejo.PullRequest{}, translateForgejoRead(err)
	}
	return repo, pr, nil
}

// GetPullDiff returns a pull request's changes as parsed diff files.
func (s *Service) GetPullDiff(ctx context.Context, actor Actor, projectSlug, repoSlug string, number int) (db.Repository, []forgejo.DiffFile, error) {
	repo, be, err := s.resolvePullRepo(ctx, actor, projectSlug, repoSlug)
	if err != nil {
		return db.Repository{}, nil, err
	}
	if be.engine {
		pr, _, err := s.engineGetPull(ctx, repo, be, number)
		if err != nil {
			return db.Repository{}, nil, err
		}
		files, err := s.enginePullDiff(ctx, be, pr)
		return repo, files, err
	}
	diff, err := s.forgejo.GetPullDiff(ctx, be.owner, be.name, number)
	if err != nil {
		return db.Repository{}, nil, translateForgejoRead(err)
	}
	return repo, forgejo.ParseUnifiedDiff(diff), nil
}

// ListPullComments returns the conversation comments on a pull request.
func (s *Service) ListPullComments(ctx context.Context, actor Actor, projectSlug, repoSlug string, number int) ([]forgejo.IssueComment, error) {
	_, be, err := s.resolvePullRepo(ctx, actor, projectSlug, repoSlug)
	if err != nil {
		return nil, err
	}
	if be.engine {
		pr, _, err := s.engineGetPull(ctx, db.Repository{}, be, number)
		if err != nil {
			return nil, err
		}
		rows, err := s.store.ListPullComments(ctx, pr.ID)
		if err != nil {
			return nil, err
		}
		out := make([]forgejo.IssueComment, 0, len(rows))
		for _, c := range rows {
			out = append(out, forgejo.IssueComment{
				ID: c.ID, Body: c.Body,
				User:      &forgejo.User{Login: c.AuthorName, FullName: c.AuthorName, Type: "User"},
				CreatedAt: c.CreatedAt, UpdatedAt: c.CreatedAt,
			})
		}
		return out, nil
	}
	comments, err := s.forgejo.ListIssueComments(ctx, be.owner, be.name, number)
	if err != nil {
		return nil, translateForgejoRead(err)
	}
	return comments, nil
}

// CreatePull opens a pull request from head into base, attributed to the actor.
func (s *Service) CreatePull(ctx context.Context, actor Actor, projectSlug, repoSlug string, in CreatePullInput) (db.Repository, forgejo.PullRequest, error) {
	title := strings.TrimSpace(in.Title)
	head := strings.TrimSpace(in.Head)
	base := strings.TrimSpace(in.Base)
	if title == "" {
		return db.Repository{}, forgejo.PullRequest{}, fmt.Errorf("%w: a title is required", ErrInvalidInput)
	}
	if head == "" || base == "" {
		return db.Repository{}, forgejo.PullRequest{}, fmt.Errorf("%w: both a source and target branch are required", ErrInvalidInput)
	}
	if head == base {
		return db.Repository{}, forgejo.PullRequest{}, fmt.Errorf("%w: the source and target branch must differ", ErrInvalidInput)
	}

	repo, be, err := s.resolvePullRepo(ctx, actor, projectSlug, repoSlug)
	if err != nil {
		return db.Repository{}, forgejo.PullRequest{}, err
	}
	if be.engine {
		dto, err := s.engineCreatePull(ctx, repo, be, actor, CreatePullInput{
			Title: title, Body: strings.TrimSpace(in.Body), Head: head, Base: base,
		})
		return repo, dto, err
	}
	asUser := s.actingForgejoUser(ctx, be.owner, be.name, actor)
	pr, err := s.forgejo.CreatePull(ctx, be.owner, be.name, asUser, forgejo.CreatePullOptions{
		Title: title,
		Body:  strings.TrimSpace(in.Body),
		Head:  head,
		Base:  base,
	})
	if err != nil {
		return db.Repository{}, forgejo.PullRequest{}, translateForgejoWrite(err)
	}
	return repo, pr, nil
}

// CreatePullComment adds a comment to a pull request, attributed to the actor.
func (s *Service) CreatePullComment(ctx context.Context, actor Actor, projectSlug, repoSlug string, number int, body string) (forgejo.IssueComment, error) {
	repo, be, err := s.resolvePullRepo(ctx, actor, projectSlug, repoSlug)
	if err != nil {
		return forgejo.IssueComment{}, err
	}
	body = strings.TrimSpace(body)
	if body == "" {
		return forgejo.IssueComment{}, fmt.Errorf("%w: a comment cannot be empty", ErrInvalidInput)
	}
	if be.engine {
		login, _, err := s.actorProfile(ctx, actor)
		if err != nil {
			return forgejo.IssueComment{}, err
		}
		pr, _, err := s.engineGetPull(ctx, repo, be, number)
		if err != nil {
			return forgejo.IssueComment{}, err
		}
		row, err := s.store.InsertPullComment(ctx, pr.ID, pr.TenantID, actor.UserID, login, body)
		if err != nil {
			return forgejo.IssueComment{}, err
		}
		_ = s.store.TouchPull(ctx, pr.ID)
		return forgejo.IssueComment{
			ID: row.ID, Body: row.Body,
			User:      &forgejo.User{Login: row.AuthorName, FullName: row.AuthorName, Type: "User"},
			CreatedAt: row.CreatedAt, UpdatedAt: row.CreatedAt,
		}, nil
	}
	asUser := s.actingForgejoUser(ctx, be.owner, be.name, actor)
	comment, err := s.forgejo.CreateIssueComment(ctx, be.owner, be.name, number, asUser, body)
	if err != nil {
		return forgejo.IssueComment{}, translateForgejoWrite(err)
	}
	return comment, nil
}

// ListPullCommits returns the commits contained in a pull request.
func (s *Service) ListPullCommits(ctx context.Context, actor Actor, projectSlug, repoSlug string, number int) (db.Repository, []forgejo.Commit, error) {
	repo, be, err := s.resolvePullRepo(ctx, actor, projectSlug, repoSlug)
	if err != nil {
		return db.Repository{}, nil, err
	}
	if be.engine {
		pr, _, err := s.engineGetPull(ctx, repo, be, number)
		if err != nil {
			return db.Repository{}, nil, err
		}
		commits, err := s.enginePullCommits(ctx, be, pr)
		return repo, commits, err
	}
	commits, err := s.forgejo.ListPullCommits(ctx, be.owner, be.name, number)
	if err != nil {
		return db.Repository{}, nil, translateForgejoRead(err)
	}
	return repo, commits, nil
}

// LineComment is a line-anchored review comment on a pull request's diff.
type LineComment struct {
	ID        int64
	Path      string
	Line      int
	Body      string
	Author    *forgejo.User
	CreatedAt time.Time
}

// ListLineComments returns every line-anchored review comment on a pull request,
// flattened across all of its reviews and ordered by creation time.
func (s *Service) ListLineComments(ctx context.Context, actor Actor, projectSlug, repoSlug string, number int) (db.Repository, []LineComment, error) {
	repo, be, err := s.resolvePullRepo(ctx, actor, projectSlug, repoSlug)
	if err != nil {
		return db.Repository{}, nil, err
	}
	if be.engine {
		pr, _, err := s.engineGetPull(ctx, repo, be, number)
		if err != nil {
			return db.Repository{}, nil, err
		}
		rows, err := s.store.ListPullLineComments(ctx, pr.ID)
		if err != nil {
			return db.Repository{}, nil, err
		}
		var out []LineComment
		for _, c := range rows {
			created := c.CreatedAt
			out = append(out, LineComment{
				ID: c.ID, Path: c.Path, Line: c.Line, Body: c.Body,
				Author: &forgejo.User{Login: c.AuthorName, FullName: c.AuthorName, Type: "User"},
				CreatedAt: created,
			})
		}
		return repo, out, nil
	}
	reviews, err := s.forgejo.ListReviews(ctx, be.owner, be.name, number)
	if err != nil {
		return db.Repository{}, nil, translateForgejoRead(err)
	}
	var out []LineComment
	for _, rv := range reviews {
		comments, err := s.forgejo.ListReviewComments(ctx, be.owner, be.name, number, rv.ID)
		if err != nil {
			return db.Repository{}, nil, translateForgejoRead(err)
		}
		for _, cm := range comments {
			out = append(out, LineComment{
				ID:        cm.ID,
				Path:      cm.Path,
				Line:      cm.Position,
				Body:      cm.Body,
				Author:    cm.User,
				CreatedAt: cm.CreatedAt,
			})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return repo, out, nil
}

// CreateLineComment posts a single line-anchored comment on a pull request's
// diff, attributed to the actor. line is the line number in the new version of
// the file (matching the diff's new-side gutter).
func (s *Service) CreateLineComment(ctx context.Context, actor Actor, projectSlug, repoSlug string, number int, path string, line int, body string) (LineComment, error) {
	repo, be, err := s.resolvePullRepo(ctx, actor, projectSlug, repoSlug)
	if err != nil {
		return LineComment{}, err
	}
	if be.engine {
		login, _, err := s.actorProfile(ctx, actor)
		if err != nil {
			return LineComment{}, err
		}
		pr, _, err := s.engineGetPull(ctx, repo, be, number)
		if err != nil {
			return LineComment{}, err
		}
		row, err := s.store.InsertPullLineComment(ctx, pr.ID, pr.TenantID, actor.UserID, login, path, line, body)
		if err != nil {
			return LineComment{}, err
		}
		_ = s.store.TouchPull(ctx, pr.ID)
		created := row.CreatedAt
		return LineComment{
			ID: row.ID, Path: row.Path, Line: row.Line, Body: row.Body,
			Author: &forgejo.User{Login: row.AuthorName, FullName: row.AuthorName, Type: "User"},
			CreatedAt: created,
		}, nil
	}
	body = strings.TrimSpace(body)
	if body == "" {
		return LineComment{}, fmt.Errorf("%w: a comment cannot be empty", ErrInvalidInput)
	}
	if strings.TrimSpace(path) == "" || line <= 0 {
		return LineComment{}, fmt.Errorf("%w: a file path and line are required", ErrInvalidInput)
	}
	asUser := s.actingForgejoUser(ctx, be.owner, be.name, actor)
	review, err := s.forgejo.CreateReview(ctx, be.owner, be.name, number, asUser, forgejo.CreateReviewOptions{
		Event:    forgejo.ReviewComment,
		Comments: []forgejo.ReviewCommentInput{{Path: path, Body: body, NewPosition: line}},
	})
	if err != nil {
		return LineComment{}, translateForgejoWrite(err)
	}
	// Surface the freshly created comment so the caller can render it without a
	// second round-trip; the review carries exactly the one comment we sent.
	comments, err := s.forgejo.ListReviewComments(ctx, be.owner, be.name, number, review.ID)
	if err == nil && len(comments) > 0 {
		cm := comments[len(comments)-1]
		return LineComment{ID: cm.ID, Path: cm.Path, Line: cm.Position, Body: cm.Body, Author: cm.User, CreatedAt: cm.CreatedAt}, nil
	}
	return LineComment{Path: path, Line: line, Body: body}, nil
}

// MergePull merges a pull request using method ("merge", "squash", or "rebase")
// and returns the refreshed pull request. Before merging it enforces any branch
// policy governing the PR's base branch (required approvals, no outstanding
// change requests) — the authoritative gate for the PR flow.
func (s *Service) MergePull(ctx context.Context, actor Actor, projectSlug, repoSlug string, number int, method string) (db.Repository, forgejo.PullRequest, error) {
	repo, be, err := s.resolvePullRepo(ctx, actor, projectSlug, repoSlug)
	if err != nil {
		return db.Repository{}, forgejo.PullRequest{}, err
	}
	if be.engine {
		merged, err := s.engineMergePull(ctx, repo, be, actor, number, method)
		return repo, merged, err
	}
	if method == "" {
		method = "merge"
	}
	if !validMergeMethods[method] {
		return db.Repository{}, forgejo.PullRequest{}, fmt.Errorf("%w: merge method must be merge, squash or rebase", ErrInvalidInput)
	}
	pr, err := s.forgejo.GetPull(ctx, be.owner, be.name, number)
	if err != nil {
		return db.Repository{}, forgejo.PullRequest{}, translateForgejoRead(err)
	}
	if err := s.enforceMergeGate(ctx, repo, be.owner, be.name, pr); err != nil {
		return db.Repository{}, forgejo.PullRequest{}, err
	}
	asUser := s.actingForgejoUser(ctx, be.owner, be.name, actor)
	if err := s.forgejo.MergePull(ctx, be.owner, be.name, number, asUser, forgejo.MergePullOptions{Do: method}); err != nil {
		return db.Repository{}, forgejo.PullRequest{}, translateForgejoWrite(err)
	}
	merged, err := s.forgejo.GetPull(ctx, be.owner, be.name, number)
	if err != nil {
		return db.Repository{}, forgejo.PullRequest{}, translateForgejoRead(err)
	}
	return repo, merged, nil
}

// ReviewState is the merge-readiness of a pull request against the policies that
// govern its base branch, composed across tenant/project/repo scope.
type ReviewState struct {
	// Applies is true when at least one branch policy governs the base branch.
	// When false no gate is shown (the PR may merge freely).
	Applies bool
	// Pattern is the selector of the closest applicable policy, for display.
	Pattern string
	// RequiredApprovals is the strictest approval threshold across applicable
	// scopes (the number the UI shows as required).
	RequiredApprovals int
	Approvals         int
	ChangesRequested  int
	Blocked           bool
	// Reason is the first denial message, kept for back-compat with single-reason
	// callers. Denials carries the full, scope-tagged list for the explain UX.
	Reason  string
	Denials []policy.Denial
	// Pipeline status check fields.
	RequireStatusChecks bool
	AllChecksPass       bool
	CheckCount          int
}

// ReviewsAndState returns a pull request's reviews together with the policy gate
// evaluated against its base branch — what the frontend needs to render review
// status and the merge box in a single call.
func (s *Service) ReviewsAndState(ctx context.Context, actor Actor, projectSlug, repoSlug string, number int) ([]forgejo.Review, ReviewState, error) {
	repo, be, err := s.resolvePullRepo(ctx, actor, projectSlug, repoSlug)
	if err != nil {
		return nil, ReviewState{}, err
	}
	if be.engine {
		prRow, dto, err := s.engineGetPull(ctx, repo, be, number)
		if err != nil {
			return nil, ReviewState{}, err
		}
		rows, err := s.store.ListPullReviews(ctx, prRow.ID)
		if err != nil {
			return nil, ReviewState{}, err
		}
		reviews := reviewsToDTO(rows)
		state, err := s.branchGate(ctx, repo, dto, reviews)
		if err != nil {
			return nil, ReviewState{}, err
		}
		return reviews, state, nil
	}
	pr, err := s.forgejo.GetPull(ctx, be.owner, be.name, number)
	if err != nil {
		return nil, ReviewState{}, translateForgejoRead(err)
	}
	reviews, err := s.forgejo.ListReviews(ctx, be.owner, be.name, number)
	if err != nil {
		return nil, ReviewState{}, translateForgejoRead(err)
	}
	state, err := s.branchGate(ctx, repo, pr, reviews)
	if err != nil {
		return nil, ReviewState{}, err
	}
	return reviews, state, nil
}

// enforceMergeGate returns ErrPolicyViolation when a branch policy blocks merging
// pr, and nil when the merge is allowed (including when no policy applies).
func (s *Service) enforceMergeGate(ctx context.Context, repo db.Repository, owner, name string, pr forgejo.PullRequest) error {
	reviews, err := s.forgejo.ListReviews(ctx, owner, name, pr.Number)
	if err != nil {
		return translateForgejoRead(err)
	}
	state, err := s.branchGate(ctx, repo, pr, reviews)
	if err != nil {
		return err
	}
	if state.Blocked {
		return fmt.Errorf("%w: %s", ErrPolicyViolation, joinDenials(state.Denials))
	}
	return nil
}

// branchGate composes the branch policies governing pr's base branch into a
// merge verdict via the policy Evaluator. Unanimous-allow composition is the
// live semantics here: every applicable policy (tenant, project, repo) must
// allow, so a narrower scope can only add restrictions. Ref-freshness
// (requireUpToDate) is owned by Forgejo's BlockOnOutdatedBranch, so the gate
// reports the branch as up to date and abstains on that check.
func (s *Service) branchGate(ctx context.Context, repo db.Repository, pr forgejo.PullRequest, reviews []forgejo.Review) (ReviewState, error) {
	project, err := s.store.GetProjectByID(ctx, repo.ProjectID)
	if err != nil {
		return ReviewState{}, fmt.Errorf("load project: %w", err)
	}
	rows, err := s.store.ListEffectivePolicies(ctx, db.ListEffectivePoliciesParams{
		Kind:      string(policy.KindBranch),
		ScopeID:   repo.ID,
		ScopeID_2: project.ID,
		ScopeID_3: project.TenantID,
	})
	if err != nil {
		return ReviewState{}, fmt.Errorf("load branch policies: %w", err)
	}
	scoped, err := scopedBranchPolicies(rows)
	if err != nil {
		return ReviewState{}, err
	}
	allChecksPass, checkCount := s.headChecksPass(ctx, repo.ID, pr.Head.SHA)
	return s.branchState(ctx, scoped, scopedPolicyInputs(rows), pr, reviews, allChecksPass, checkCount)
}

// headChecksPass reports whether all pipeline runs recorded for the given
// commit SHA in the given repo have a "success" status. When no runs exist
// it returns (true, 0): the absence of CI is not treated as a check failure so
// repos without pipelines are not blocked by a status-check policy.
func (s *Service) headChecksPass(ctx context.Context, repoID uuid.UUID, sha string) (bool, int) {
	if sha == "" {
		return true, 0
	}
	runs, err := s.store.ListRunsByCommitSHA(ctx, repoID, sha)
	if err != nil || len(runs) == 0 {
		return true, 0
	}
	for _, r := range runs {
		if r.Status != pipeline.StatusSuccess {
			return false, len(runs)
		}
	}
	return true, len(runs)
}

// branchState composes an already-loaded policy set into a merge verdict. It is
// the DB-free core of branchGate: callers supply the decoded policies (for the
// display summary) and the raw inputs (for the Evaluator) plus the PR facts.
func (s *Service) branchState(ctx context.Context, scoped []policy.ScopedBranch, inputs []policy.ScopedPolicy, pr forgejo.PullRequest, reviews []forgejo.Review, allChecksPass bool, checkCount int) (ReviewState, error) {
	info := policy.ApplicableBranchInfo(scoped, pr.Base.Ref)
	if !info.Applies {
		return ReviewState{}, nil
	}
	approvals, changesRequested := summarizeReviews(reviews, pr.User, info.DismissStale)
	facts := policy.Context{
		Now: time.Now(),
		Branch: &policy.BranchFacts{
			BaseRef:          pr.Base.Ref,
			HeadRef:          pr.Head.Ref,
			Approvals:        approvals,
			ChangesRequested: changesRequested,
			UpToDate:         true,
			AllChecksPass:    allChecksPass,
		},
	}
	decision, err := s.evaluator.Evaluate(ctx, policy.KindBranch, inputs, facts)
	if err != nil {
		return ReviewState{}, fmt.Errorf("evaluate branch policy: %w", err)
	}
	state := ReviewState{
		Applies:             true,
		Pattern:             info.Pattern,
		RequiredApprovals:   info.RequiredApprovals,
		Approvals:           approvals,
		ChangesRequested:    changesRequested,
		Blocked:             !decision.Allow,
		Denials:             decision.Denials,
		RequireStatusChecks: info.RequireStatusChecks,
		AllChecksPass:       allChecksPass,
		CheckCount:          checkCount,
	}
	if len(decision.Denials) > 0 {
		state.Reason = decision.Denials[0].Message
	}
	return state, nil
}

// joinDenials renders a composed decision's denial messages into a single line
// for the merge error.
func joinDenials(denials []policy.Denial) string {
	msgs := make([]string, 0, len(denials))
	for _, d := range denials {
		msgs = append(msgs, d.Message)
	}
	return strings.Join(msgs, "; ")
}

// summarizeReviews tallies the latest non-dismissed review per user (excluding
// the PR author). Stale approvals are ignored when the policy dismisses them.
func summarizeReviews(reviews []forgejo.Review, author *forgejo.User, dismissStale bool) (approvals, changesRequested int) {
	type latest struct {
		state string
		stale bool
	}
	authorLogin := ""
	if author != nil {
		authorLogin = author.Login
	}
	byUser := map[string]latest{}
	for _, rv := range reviews {
		if rv.User == nil || rv.Dismissed {
			continue
		}
		if rv.State != forgejo.ReviewApproved && rv.State != forgejo.ReviewRequestChanges {
			continue
		}
		if rv.User.Login == authorLogin {
			continue
		}
		// Reviews arrive in submission order; the last one wins.
		byUser[rv.User.Login] = latest{state: rv.State, stale: rv.Stale}
	}
	for _, l := range byUser {
		switch l.state {
		case forgejo.ReviewApproved:
			if dismissStale && l.stale {
				continue
			}
			approvals++
		case forgejo.ReviewRequestChanges:
			changesRequested++
		}
	}
	return approvals, changesRequested
}

// pull request, attributed to the actor.
func (s *Service) CreatePullReview(ctx context.Context, actor Actor, projectSlug, repoSlug string, number int, event, body string) (forgejo.Review, error) {
	repo, be, err := s.resolvePullRepo(ctx, actor, projectSlug, repoSlug)
	if err != nil {
		return forgejo.Review{}, err
	}
	event = strings.ToUpper(strings.TrimSpace(event))
	if !validReviewEvents[event] {
		return forgejo.Review{}, fmt.Errorf("%w: review must approve, request changes, or comment", ErrInvalidInput)
	}
	body = strings.TrimSpace(body)
	if event == forgejo.ReviewComment && body == "" {
		return forgejo.Review{}, fmt.Errorf("%w: a comment review requires a body", ErrInvalidInput)
	}
	if be.engine {
		login, _, err := s.actorProfile(ctx, actor)
		if err != nil {
			return forgejo.Review{}, err
		}
		pr, _, err := s.engineGetPull(ctx, repo, be, number)
		if err != nil {
			return forgejo.Review{}, err
		}
		row, err := s.store.InsertPullReview(ctx, pr.ID, pr.TenantID, actor.UserID, login, event, body)
		if err != nil {
			return forgejo.Review{}, err
		}
		_ = s.store.TouchPull(ctx, pr.ID)
		return reviewDTO(row), nil
	}
	asUser := s.actingForgejoUser(ctx, be.owner, be.name, actor)
	review, err := s.forgejo.CreateReview(ctx, be.owner, be.name, number, asUser, forgejo.CreateReviewOptions{Event: event, Body: body})
	if err != nil {
		return forgejo.Review{}, translateForgejoWrite(err)
	}
	return review, nil
}

// actingForgejoUser resolves the actor's Forgejo login and ensures they have
// write access to the repository so a sudo'd write is attributed to them. It
// returns "" when the user has no linked Forgejo account or access can't be
// granted, in which case the caller falls back to an admin-attributed write.
func (s *Service) actingForgejoUser(ctx context.Context, owner, name string, actor Actor) string {
	user, err := s.store.GetUserByID(ctx, actor.UserID)
	if err != nil || !user.ForgejoUsername.Valid || user.ForgejoUsername.String == "" {
		return ""
	}
	login := user.ForgejoUsername.String
	if err := s.forgejo.AddCollaborator(ctx, owner, name, login, "write"); err != nil {
		s.logger.Warn("could not ensure forgejo collaborator for attribution",
			"user", login, "repo", owner+"/"+name, "error", err)
		return ""
	}
	return login
}

// translateForgejoWrite maps a Forgejo write error to a platform sentinel by
// HTTP status: 404→NotFound, 409→Conflict, 422→InvalidInput, 403→Forbidden,
// network errors (connection refused, timeout) → ErrUnavailable.
func translateForgejoWrite(err error) error {
	switch forgejo.StatusCode(err) {
	case http.StatusNotFound:
		return ErrNotFound
	case http.StatusConflict:
		return fmt.Errorf("%w: %s", ErrConflict, err.Error())
	case http.StatusMethodNotAllowed:
		// Forgejo answers 405 when merging an already-merged/closed PR
		return fmt.Errorf("%w: pull request is already merged or closed", ErrConflict)
	case http.StatusUnprocessableEntity:
		return fmt.Errorf("%w: %s", ErrInvalidInput, err.Error())
	case http.StatusForbidden:
		return ErrForbidden
	default:
		if forgejo.IsNetworkError(err) {
			return fmt.Errorf("%w: %s", ErrUnavailable, err)
		}
		return err
	}
}
