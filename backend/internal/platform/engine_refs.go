package platform

import (
	"context"
	"fmt"
	"strings"

	"github.com/nielsuitterdijk22/quill/internal/store"
	"github.com/nielsuitterdijk22/quill/internal/store/db"
	"github.com/nielsuitterdijk22/quill/internal/workitemrefs"
)

// WithWebURL sets the public web base used for links in cross-product refs
// (e.g. a pull request's page, shown in Tempo). Empty means links are omitted.
func (s *Service) WithWebURL(u string) *Service {
	s.webURL = strings.TrimRight(strings.TrimSpace(u), "/")
	return s
}

// pullRefState is the state Tempo is told: open, closed, or merged (a merged pull
// request is also closed, but Tempo needs to tell the two apart).
func pullRefState(pr store.PullRequest) string {
	switch {
	case pr.Merged:
		return "merged"
	case pr.State == "open":
		return "open"
	default:
		return "closed"
	}
}

// enqueuePullRefs tells Tempo about a pull request that mentions work-item keys
// (in its title, body or branch name) and its current state, so Tempo can link it,
// show the item as in review and resolve it on merge. Best-effort and async: the
// push goes through the outbox, a failure to enqueue is logged and never fails
// the pull request operation.
func (s *Service) enqueuePullRefs(ctx context.Context, repo db.Repository, pr store.PullRequest) {
	ctx = context.WithoutCancel(ctx)
	ref, ok := workitemrefs.PullRequestRef(repo.Slug, workitemrefs.PullRequest{
		Number: pr.Number,
		Title:  pr.Title,
		Body:   pr.Body,
		State:  pullRefState(pr),
		URL:    s.pullWebURL(ctx, repo, pr.Number),
		Branch: pr.HeadRef,
		Author: pr.CreatedByName,
	})
	if !ok {
		return
	}
	if err := workitemrefs.Enqueue(ctx, s.store, workitemrefs.RefPush{QuillProjectID: repo.ProjectID, Refs: []workitemrefs.Ref{ref}}); err != nil {
		s.logger.Warn("work-item ref enqueue failed", "repo", repo.Slug, "pull", pr.Number, "error", err)
	}
}

// pullWebURL is the pull request's page in the Quill web app, or "" when no web
// base is configured.
func (s *Service) pullWebURL(ctx context.Context, repo db.Repository, number int) string {
	if s.webURL == "" {
		return ""
	}
	project, err := s.store.GetProjectByID(ctx, repo.ProjectID)
	if err != nil {
		return ""
	}
	return fmt.Sprintf("%s/projects/%s/repos/%s/pulls/%d", s.webURL, project.Slug, repo.Slug, number)
}
