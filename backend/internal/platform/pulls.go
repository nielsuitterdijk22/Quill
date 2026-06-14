package platform

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/nielsuitterdijk22/quill/internal/forgejo"
	"github.com/nielsuitterdijk22/quill/internal/store/db"
)

// This file exposes pull-request operations on top of Forgejo. Reads use the
// admin token (Quill enforces visibility via org membership, exactly like code
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
func (s *Service) ListPulls(ctx context.Context, actor Actor, orgSlug, repoSlug, state string) (db.Repository, []forgejo.PullRequest, error) {
	repo, owner, name, err := s.resolveRepo(ctx, actor, orgSlug, repoSlug, true)
	if err != nil {
		return db.Repository{}, nil, err
	}
	pulls, err := s.forgejo.ListPulls(ctx, owner, name, state, 0)
	if err != nil {
		return db.Repository{}, nil, translateForgejoRead(err)
	}
	return repo, pulls, nil
}

// GetPull returns a single pull request by number.
func (s *Service) GetPull(ctx context.Context, actor Actor, orgSlug, repoSlug string, number int) (db.Repository, forgejo.PullRequest, error) {
	repo, owner, name, err := s.resolveRepo(ctx, actor, orgSlug, repoSlug, true)
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
func (s *Service) GetPullDiff(ctx context.Context, actor Actor, orgSlug, repoSlug string, number int) (db.Repository, []forgejo.DiffFile, error) {
	repo, owner, name, err := s.resolveRepo(ctx, actor, orgSlug, repoSlug, true)
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
func (s *Service) ListPullComments(ctx context.Context, actor Actor, orgSlug, repoSlug string, number int) ([]forgejo.IssueComment, error) {
	_, owner, name, err := s.resolveRepo(ctx, actor, orgSlug, repoSlug, true)
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
func (s *Service) CreatePull(ctx context.Context, actor Actor, orgSlug, repoSlug string, in CreatePullInput) (db.Repository, forgejo.PullRequest, error) {
	repo, owner, name, err := s.resolveRepo(ctx, actor, orgSlug, repoSlug, true)
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
func (s *Service) CreatePullComment(ctx context.Context, actor Actor, orgSlug, repoSlug string, number int, body string) (forgejo.IssueComment, error) {
	_, owner, name, err := s.resolveRepo(ctx, actor, orgSlug, repoSlug, true)
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

// MergePull merges a pull request using method ("merge", "squash", or "rebase")
// and returns the refreshed pull request. Before merging it enforces any branch
// policy governing the PR's base branch (required approvals, no outstanding
// change requests) — the authoritative gate for the PR flow.
func (s *Service) MergePull(ctx context.Context, actor Actor, orgSlug, repoSlug string, number int, method string) (db.Repository, forgejo.PullRequest, error) {
	repo, owner, name, err := s.resolveRepo(ctx, actor, orgSlug, repoSlug, true)
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
	Policy           *db.BranchPolicy
	Approvals        int
	ChangesRequested int
	Blocked          bool
	Reason           string
}

// ReviewsAndState returns a pull request's reviews together with the policy gate
// evaluated against its base branch — what the frontend needs to render review
// status and the merge box in a single call.
func (s *Service) ReviewsAndState(ctx context.Context, actor Actor, orgSlug, repoSlug string, number int) ([]forgejo.Review, ReviewState, error) {
	repo, owner, name, err := s.resolveRepo(ctx, actor, orgSlug, repoSlug, true)
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
	policies, err := s.store.ListBranchPoliciesByRepo(ctx, repo.ID)
	if err != nil {
		return nil, ReviewState{}, fmt.Errorf("load branch policies: %w", err)
	}
	state := gateFromReviews(matchBranchPolicy(policies, pr.Base.Ref), pr.User, reviews)
	return reviews, state, nil
}

// enforceMergeGate returns ErrPolicyViolation when a branch policy blocks merging
// pr, and nil when the merge is allowed (including when no policy applies).
func (s *Service) enforceMergeGate(ctx context.Context, repo db.Repository, owner, name string, pr forgejo.PullRequest) error {
	policies, err := s.store.ListBranchPoliciesByRepo(ctx, repo.ID)
	if err != nil {
		return fmt.Errorf("load branch policies: %w", err)
	}
	policy := matchBranchPolicy(policies, pr.Base.Ref)
	if policy == nil {
		return nil
	}
	reviews, err := s.forgejo.ListReviews(ctx, owner, name, pr.Number)
	if err != nil {
		return translateForgejoRead(err)
	}
	state := gateFromReviews(policy, pr.User, reviews)
	if state.Blocked {
		return fmt.Errorf("%w: %s", ErrPolicyViolation, state.Reason)
	}
	return nil
}

// gateFromReviews computes the merge-readiness verdict for a policy against a
// pull request's reviews. A nil policy means no gate applies (never blocked).
func gateFromReviews(policy *db.BranchPolicy, author *forgejo.User, reviews []forgejo.Review) ReviewState {
	if policy == nil {
		return ReviewState{}
	}
	approvals, changesRequested := summarizeReviews(reviews, author, policy.DismissStaleApprovals)
	state := ReviewState{Policy: policy, Approvals: approvals, ChangesRequested: changesRequested}
	switch {
	case changesRequested > 0:
		state.Blocked = true
		state.Reason = "changes have been requested and must be resolved"
	case approvals < int(policy.RequiredApprovals):
		state.Blocked = true
		state.Reason = fmt.Sprintf("%d of %d required approvals", approvals, policy.RequiredApprovals)
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
func (s *Service) CreatePullReview(ctx context.Context, actor Actor, orgSlug, repoSlug string, number int, event, body string) (forgejo.Review, error) {
	_, owner, name, err := s.resolveRepo(ctx, actor, orgSlug, repoSlug, true)
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
