package platform

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/nielsuitterdijk22/quill/internal/forgejo"
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
	repo, owner, name, err := s.resolveRepo(ctx, actor, projectSlug, repoSlug, true)
	if err != nil {
		return db.Repository{}, nil, err
	}
	pulls, err := s.forgejo.ListPulls(ctx, owner, name, state, 0)
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
	if !s.forgejoEnabled() {
		return 0, nil
	}

	projects, err := s.store.ListProjects(ctx, db.ListProjectsParams{Limit: 200, Offset: 0})
	if err != nil {
		return 0, fmt.Errorf("list projects: %w", err)
	}

	type target struct{ owner, name string }
	var targets []target
	for _, project := range projects {
		// Only count repos in projects the actor can see; skip the rest silently so the
		// total matches what the dashboard would list for this user.
		if err := s.authorizeProjectMember(ctx, actor, project.ID); err != nil {
			if errors.Is(err, ErrForbidden) {
				continue
			}
			return 0, err
		}
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
	return int(total), nil
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

// ListOpenPulls returns pull requests across every repository in the
// projects the actor belongs to, newest-updated first. Like
// OpenPullRequestCount it is best-effort: a repository whose pulls can't be read
// is skipped and logged, mirroring the dashboard's tolerance for partial data.
// The optional input narrows the result by state and/or project.
func (s *Service) ListOpenPulls(ctx context.Context, actor Actor, in ListOpenPullsInput) ([]RepoPull, error) {
	if !s.forgejoEnabled() {
		return nil, nil
	}
	state := in.State
	if state == "" {
		state = "open"
	}

	projects, err := s.store.ListProjects(ctx, db.ListProjectsParams{Limit: 200, Offset: 0})
	if err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}

	type target struct {
		owner, name, projectSlug, repoSlug, repoName string
	}
	var targets []target
	for _, project := range projects {
		if in.ProjectSlug != "" && project.Slug != in.ProjectSlug {
			continue
		}
		// Only list repos in projects the actor can see; skip the rest silently so the
		// result matches what the dashboard would show for this user.
		if err := s.authorizeProjectMember(ctx, actor, project.ID); err != nil {
			if errors.Is(err, ErrForbidden) {
				continue
			}
			return nil, err
		}
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

	var all []RepoPull
	for _, r := range results {
		all = append(all, r...)
	}
	// Newest-updated first across all repositories.
	sort.Slice(all, func(i, j int) bool { return all[i].Pull.UpdatedAt.After(all[j].Pull.UpdatedAt) })
	return all, nil
}

// GetPull returns a single pull request by number.
func (s *Service) GetPull(ctx context.Context, actor Actor, projectSlug, repoSlug string, number int) (db.Repository, forgejo.PullRequest, error) {
	repo, owner, name, err := s.resolveRepo(ctx, actor, projectSlug, repoSlug, true)
	if err != nil {
		return db.Repository{}, forgejo.PullRequest{}, err
	}
	pr, err := s.forgejo.GetPull(ctx, owner, name, number)
	if err != nil {
		return db.Repository{}, forgejo.PullRequest{}, translateForgejoRead(err)
	}
	return repo, pr, nil
}

// GetPullDiff returns a pull request's changes as parsed diff files.
func (s *Service) GetPullDiff(ctx context.Context, actor Actor, projectSlug, repoSlug string, number int) (db.Repository, []forgejo.DiffFile, error) {
	repo, owner, name, err := s.resolveRepo(ctx, actor, projectSlug, repoSlug, true)
	if err != nil {
		return db.Repository{}, nil, err
	}
	diff, err := s.forgejo.GetPullDiff(ctx, owner, name, number)
	if err != nil {
		return db.Repository{}, nil, translateForgejoRead(err)
	}
	return repo, forgejo.ParseUnifiedDiff(diff), nil
}

// ListPullComments returns the conversation comments on a pull request.
func (s *Service) ListPullComments(ctx context.Context, actor Actor, projectSlug, repoSlug string, number int) ([]forgejo.IssueComment, error) {
	_, owner, name, err := s.resolveRepo(ctx, actor, projectSlug, repoSlug, true)
	if err != nil {
		return nil, err
	}
	comments, err := s.forgejo.ListIssueComments(ctx, owner, name, number)
	if err != nil {
		return nil, translateForgejoRead(err)
	}
	return comments, nil
}

// CreatePull opens a pull request from head into base, attributed to the actor.
func (s *Service) CreatePull(ctx context.Context, actor Actor, projectSlug, repoSlug string, in CreatePullInput) (db.Repository, forgejo.PullRequest, error) {
	repo, owner, name, err := s.resolveRepo(ctx, actor, projectSlug, repoSlug, true)
	if err != nil {
		return db.Repository{}, forgejo.PullRequest{}, err
	}
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

	asUser := s.actingForgejoUser(ctx, owner, name, actor)
	pr, err := s.forgejo.CreatePull(ctx, owner, name, asUser, forgejo.CreatePullOptions{
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
	_, owner, name, err := s.resolveRepo(ctx, actor, projectSlug, repoSlug, true)
	if err != nil {
		return forgejo.IssueComment{}, err
	}
	body = strings.TrimSpace(body)
	if body == "" {
		return forgejo.IssueComment{}, fmt.Errorf("%w: a comment cannot be empty", ErrInvalidInput)
	}
	asUser := s.actingForgejoUser(ctx, owner, name, actor)
	comment, err := s.forgejo.CreateIssueComment(ctx, owner, name, number, asUser, body)
	if err != nil {
		return forgejo.IssueComment{}, translateForgejoWrite(err)
	}
	return comment, nil
}

// ListPullCommits returns the commits contained in a pull request.
func (s *Service) ListPullCommits(ctx context.Context, actor Actor, projectSlug, repoSlug string, number int) (db.Repository, []forgejo.Commit, error) {
	repo, owner, name, err := s.resolveRepo(ctx, actor, projectSlug, repoSlug, true)
	if err != nil {
		return db.Repository{}, nil, err
	}
	commits, err := s.forgejo.ListPullCommits(ctx, owner, name, number)
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
	repo, owner, name, err := s.resolveRepo(ctx, actor, projectSlug, repoSlug, true)
	if err != nil {
		return db.Repository{}, nil, err
	}
	reviews, err := s.forgejo.ListReviews(ctx, owner, name, number)
	if err != nil {
		return db.Repository{}, nil, translateForgejoRead(err)
	}
	var out []LineComment
	for _, rv := range reviews {
		comments, err := s.forgejo.ListReviewComments(ctx, owner, name, number, rv.ID)
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
	_, owner, name, err := s.resolveRepo(ctx, actor, projectSlug, repoSlug, true)
	if err != nil {
		return LineComment{}, err
	}
	body = strings.TrimSpace(body)
	if body == "" {
		return LineComment{}, fmt.Errorf("%w: a comment cannot be empty", ErrInvalidInput)
	}
	if strings.TrimSpace(path) == "" || line <= 0 {
		return LineComment{}, fmt.Errorf("%w: a file path and line are required", ErrInvalidInput)
	}
	asUser := s.actingForgejoUser(ctx, owner, name, actor)
	review, err := s.forgejo.CreateReview(ctx, owner, name, number, asUser, forgejo.CreateReviewOptions{
		Event:    forgejo.ReviewComment,
		Comments: []forgejo.ReviewCommentInput{{Path: path, Body: body, NewPosition: line}},
	})
	if err != nil {
		return LineComment{}, translateForgejoWrite(err)
	}
	// Surface the freshly created comment so the caller can render it without a
	// second round-trip; the review carries exactly the one comment we sent.
	comments, err := s.forgejo.ListReviewComments(ctx, owner, name, number, review.ID)
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
	repo, owner, name, err := s.resolveRepo(ctx, actor, projectSlug, repoSlug, true)
	if err != nil {
		return db.Repository{}, forgejo.PullRequest{}, err
	}
	if method == "" {
		method = "merge"
	}
	if !validMergeMethods[method] {
		return db.Repository{}, forgejo.PullRequest{}, fmt.Errorf("%w: merge method must be merge, squash or rebase", ErrInvalidInput)
	}
	pr, err := s.forgejo.GetPull(ctx, owner, name, number)
	if err != nil {
		return db.Repository{}, forgejo.PullRequest{}, translateForgejoRead(err)
	}
	if err := s.enforceMergeGate(ctx, repo, owner, name, pr); err != nil {
		return db.Repository{}, forgejo.PullRequest{}, err
	}
	asUser := s.actingForgejoUser(ctx, owner, name, actor)
	if err := s.forgejo.MergePull(ctx, owner, name, number, asUser, forgejo.MergePullOptions{Do: method}); err != nil {
		return db.Repository{}, forgejo.PullRequest{}, translateForgejoWrite(err)
	}
	merged, err := s.forgejo.GetPull(ctx, owner, name, number)
	if err != nil {
		return db.Repository{}, forgejo.PullRequest{}, translateForgejoRead(err)
	}
	return repo, merged, nil
}

// ReviewState is the merge-readiness of a pull request against the policy that
// governs its base branch.
type ReviewState struct {
	// Rule is the effective branch rule for the PR's base branch, or nil when no
	// policy applies. Pattern is the selector that produced it.
	Rule             *policy.BranchRule
	Pattern          string
	Approvals        int
	ChangesRequested int
	Blocked          bool
	Reason           string
}

// ReviewsAndState returns a pull request's reviews together with the policy gate
// evaluated against its base branch — what the frontend needs to render review
// status and the merge box in a single call.
func (s *Service) ReviewsAndState(ctx context.Context, actor Actor, projectSlug, repoSlug string, number int) ([]forgejo.Review, ReviewState, error) {
	repo, owner, name, err := s.resolveRepo(ctx, actor, projectSlug, repoSlug, true)
	if err != nil {
		return nil, ReviewState{}, err
	}
	pr, err := s.forgejo.GetPull(ctx, owner, name, number)
	if err != nil {
		return nil, ReviewState{}, translateForgejoRead(err)
	}
	reviews, err := s.forgejo.ListReviews(ctx, owner, name, number)
	if err != nil {
		return nil, ReviewState{}, translateForgejoRead(err)
	}
	rule, pattern, err := s.effectiveBranchRule(ctx, repo, pr.Base.Ref)
	if err != nil {
		return nil, ReviewState{}, err
	}
	state := gateFromReviews(rule, pattern, pr.User, reviews)
	return reviews, state, nil
}

// enforceMergeGate returns ErrPolicyViolation when a branch policy blocks merging
// pr, and nil when the merge is allowed (including when no policy applies).
func (s *Service) enforceMergeGate(ctx context.Context, repo db.Repository, owner, name string, pr forgejo.PullRequest) error {
	rule, pattern, err := s.effectiveBranchRule(ctx, repo, pr.Base.Ref)
	if err != nil {
		return err
	}
	if rule == nil {
		return nil
	}
	reviews, err := s.forgejo.ListReviews(ctx, owner, name, pr.Number)
	if err != nil {
		return translateForgejoRead(err)
	}
	state := gateFromReviews(rule, pattern, pr.User, reviews)
	if state.Blocked {
		return fmt.Errorf("%w: %s", ErrPolicyViolation, state.Reason)
	}
	return nil
}

// effectiveBranchRule resolves the branch rule governing branch on repo through
// the policy engine, folding the policies declared at the repo, its project, and
// its tenant (broad -> narrow). A narrower scope overrides a broader one unless
// the broader scope locked its policy, in which case the narrower may only
// tighten it (see internal/policy.EffectiveBranch).
func (s *Service) effectiveBranchRule(ctx context.Context, repo db.Repository, branch string) (*policy.BranchRule, string, error) {
	project, err := s.store.GetProjectByID(ctx, repo.ProjectID)
	if err != nil {
		return nil, "", fmt.Errorf("load project: %w", err)
	}
	rows, err := s.store.ListEffectivePolicies(ctx, db.ListEffectivePoliciesParams{
		Kind:      string(policy.KindBranch),
		ScopeID:   repo.ID,
		ScopeID_2: project.ID,
		ScopeID_3: project.TenantID,
	})
	if err != nil {
		return nil, "", fmt.Errorf("load branch policies: %w", err)
	}
	scoped, err := scopedBranchPolicies(rows)
	if err != nil {
		return nil, "", err
	}
	rule, pattern := policy.EffectiveBranch(scoped, branch)
	return rule, pattern, nil
}

// gateFromReviews computes the merge-readiness verdict for a branch rule against
// a pull request's reviews. A nil rule means no gate applies (never blocked).
func gateFromReviews(rule *policy.BranchRule, pattern string, author *forgejo.User, reviews []forgejo.Review) ReviewState {
	if rule == nil {
		return ReviewState{}
	}
	approvals, changesRequested := summarizeReviews(reviews, author, rule.DismissStaleApprovals)
	state := ReviewState{Rule: rule, Pattern: pattern, Approvals: approvals, ChangesRequested: changesRequested}
	switch {
	case changesRequested > 0:
		state.Blocked = true
		state.Reason = "changes have been requested and must be resolved"
	case approvals < rule.RequiredApprovals:
		state.Blocked = true
		state.Reason = fmt.Sprintf("%d of %d required approvals", approvals, rule.RequiredApprovals)
	}
	return state
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
	_, owner, name, err := s.resolveRepo(ctx, actor, projectSlug, repoSlug, true)
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
	asUser := s.actingForgejoUser(ctx, owner, name, actor)
	review, err := s.forgejo.CreateReview(ctx, owner, name, number, asUser, forgejo.CreateReviewOptions{Event: event, Body: body})
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
// HTTP status: 404→NotFound, 409→Conflict, 422→InvalidInput, 403→Forbidden.
func translateForgejoWrite(err error) error {
	switch forgejo.StatusCode(err) {
	case http.StatusNotFound:
		return ErrNotFound
	case http.StatusConflict:
		return fmt.Errorf("%w: a pull request for these branches already exists", ErrConflict)
	case http.StatusUnprocessableEntity:
		return fmt.Errorf("%w: %s", ErrInvalidInput, err.Error())
	case http.StatusForbidden:
		return ErrForbidden
	default:
		return err
	}
}
