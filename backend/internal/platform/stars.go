package platform

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/nielsuitterdijk22/quill/internal/store/db"
)

// StarRepo records that actor has starred the given repository.
func (s *Service) StarRepo(ctx context.Context, actor Actor, projectSlug, repoSlug string) error {
	repo, _, _, err := s.resolveRepo(ctx, actor, projectSlug, repoSlug, false)
	if err != nil {
		return err
	}
	if err := s.store.StarRepo(ctx, db.StarRepoParams{
		UserID: actor.UserID,
		RepoID: repo.ID,
	}); err != nil {
		return fmt.Errorf("star repo: %w", err)
	}
	return nil
}

// UnstarRepo removes actor's star from the given repository.
func (s *Service) UnstarRepo(ctx context.Context, actor Actor, projectSlug, repoSlug string) error {
	repo, _, _, err := s.resolveRepo(ctx, actor, projectSlug, repoSlug, false)
	if err != nil {
		return err
	}
	if err := s.store.UnstarRepo(ctx, db.UnstarRepoParams{
		UserID: actor.UserID,
		RepoID: repo.ID,
	}); err != nil {
		return fmt.Errorf("unstar repo: %w", err)
	}
	return nil
}

// StarInfo holds the viewer-specific star metadata for a repository.
type StarInfo struct {
	StarCount        int64
	ViewerHasStarred bool
}

// GetRepoStarInfo returns the star count and whether actor has starred the repo.
func (s *Service) GetRepoStarInfo(ctx context.Context, actor Actor, repoID db.Repository) (StarInfo, error) {
	count, err := s.store.CountRepoStars(ctx, repoID.ID)
	if err != nil {
		return StarInfo{}, fmt.Errorf("count stars: %w", err)
	}
	_, err = s.store.GetRepoStar(ctx, db.GetRepoStarParams{
		UserID: actor.UserID,
		RepoID: repoID.ID,
	})
	starred := err == nil
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return StarInfo{}, fmt.Errorf("get star: %w", err)
	}
	return StarInfo{StarCount: count, ViewerHasStarred: starred}, nil
}
