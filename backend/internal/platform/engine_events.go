package platform

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

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
	var runs []db.PipelineRun
	for _, u := range updates {
		if isZeroSHA(u.New) {
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
	}
	return runs, nil
}
