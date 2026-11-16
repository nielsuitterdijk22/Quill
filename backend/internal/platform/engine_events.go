package platform

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nielsuitterdijk22/quill/internal/pipeline"
	"github.com/nielsuitterdijk22/quill/internal/store"
	"github.com/nielsuitterdijk22/quill/internal/store/db"
)

// EngineRefUpdate is one ref a push to the own engine moved. New is "" (or
// all zeros) for a deletion.
type EngineRefUpdate struct {
	Ref string
	Old string
	New string
}

func isZeroSHA(sha string) bool {
	return strings.Trim(sha, "0") == ""
}

// HandleEnginePush is the engine's post-receive (D2 cutover, replacing the
// Forgejo system webhook): every updated branch or tag fires a `push` event
// through the same workflow matcher the webhook path used. Deletions never
// trigger. Failures are per-workflow and logged — a push never fails because
// CI could not start.
func (s *Service) HandleEnginePush(ctx context.Context, repoID uuid.UUID, updates []EngineRefUpdate) ([]db.PipelineRun, error) {
	repo, err := s.store.GetRepositoryByID(ctx, repoID)
	if err != nil {
		return nil, fmt.Errorf("lookup repo: %w", err)
	}
	src, err := s.pipelineSourceFor(ctx, repo)
	if err != nil {
		return nil, err
	}
	if !src.be.engine {
		return nil, fmt.Errorf("%w: repo %s is not engine-backed", ErrInvalidInput, repo.Slug)
	}
	// SBOM per default-branch push (not a pipeline; see sbom.go).
	if s.sbomGen != nil {
		for _, u := range updates {
			if u.Ref == "refs/heads/"+repo.DefaultBranch && !isZeroSHA(u.New) {
				if _, err := s.queueSBOM(ctx, src.be, u.New, u.Ref); err != nil {
					s.logger.Warn("sbom queue failed", "repo", src.label(), "error", err)
				}
			}
		}
	}
	// Contribution graph: index the commits the default branch gained.
	for _, u := range updates {
		if u.Ref == "refs/heads/"+repo.DefaultBranch && !isZeroSHA(u.New) {
			if _, err := s.indexCommitActivity(ctx, src.be, u.New, u.Old); err != nil {
				s.logger.Warn("commit activity index failed", "repo", src.label(), "error", err)
			}
		}
	}
	var runs []db.PipelineRun
	for _, u := range updates {
		if isZeroSHA(u.New) {
			// A deleted branch: its waiting runs have nothing left to check.
			s.withdrawRefRuns(ctx, repo.ID, u.Ref, "branch deleted")
			continue
		}
		if !strings.HasPrefix(u.Ref, "refs/heads/") && !strings.HasPrefix(u.Ref, "refs/tags/") {
			continue
		}
		rs, err := s.triggerEvent(ctx, repo, src, "push", u.Ref)
		if err != nil {
			s.logger.Warn("engine push trigger failed", "repo", src.label(), "ref", u.Ref, "error", err)
			continue
		}
		runs = append(runs, rs...)

		// A push to the source branch of open pull requests updates them:
		// run the workflows that listen for pull_request (synchronize).
		if branch, ok := strings.CutPrefix(u.Ref, "refs/heads/"); ok {
			prs, err := s.store.ListOpenPullsByHeadRef(ctx, repo.ID, branch)
			if err != nil {
				s.logger.Warn("list open pulls for push failed", "repo", src.label(), "branch", branch, "error", err)
				continue
			}
			for _, pr := range prs {
				rs, err := s.triggerEventFor(ctx, repo, src, "pull_request", u.Ref, &pipeline.PullRequestInfo{
					Number: pr.Number, BaseRef: pr.BaseRef, HeadRef: pr.HeadRef, Action: "synchronize",
				})
				if err != nil {
					s.logger.Warn("engine pull_request trigger failed", "repo", src.label(), "pull", pr.Number, "error", err)
					continue
				}
				runs = append(runs, rs...)
			}
		}
	}
	return runs, nil
}

// triggerPullRequestRuns starts the pull_request workflows for a pull request
// that was just opened or reopened. Best-effort: a failure is logged and never
// fails the pull request operation. Like a push-triggered run it builds the
// head commit.
func (s *Service) triggerPullRequestRuns(ctx context.Context, repo db.Repository, pr store.PullRequest, action string) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	src, err := s.pipelineSourceFor(ctx, repo)
	if err != nil {
		s.logger.Warn("pull_request trigger: pipeline source failed", "repo", repo.Slug, "pull", pr.Number, "error", err)
		return
	}
	if _, err := s.triggerEventFor(ctx, repo, src, "pull_request", "refs/heads/"+pr.HeadRef, &pipeline.PullRequestInfo{
		Number: pr.Number, BaseRef: pr.BaseRef, HeadRef: pr.HeadRef, Action: action,
	}); err != nil {
		s.logger.Warn("pull_request trigger failed", "repo", src.label(), "pull", pr.Number, "error", err)
	}
}

// withdrawRefRuns cancels the waiting push runs on ref (logged, best-effort).
func (s *Service) withdrawRefRuns(ctx context.Context, repoID uuid.UUID, ref, why string) {
	gone, err := s.store.WithdrawPendingRunsForRef(ctx, repoID, ref)
	if err != nil {
		s.logger.Warn("withdraw pending runs failed", "ref", ref, "error", err)
		return
	}
	if len(gone) > 0 {
		s.logger.Info("withdrew pending runs", "ref", ref, "reason", why, "runs", gone)
	}
}
