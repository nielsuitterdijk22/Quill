package projectsync

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/uuid"

	"github.com/nielsuitterdijk22/quill/internal/store/db"
)

// BackfillStore is the store surface the one-shot backfill needs. *store.Store
// satisfies it.
type BackfillStore interface {
	EventWriter
	ListProjectsWithTenant(ctx context.Context) ([]db.ListProjectsWithTenantRow, error)
	CountProjectSyncEventsByProject(ctx context.Context, projectID uuid.UUID) (int64, error)
}

// BackfillResult reports what a backfill run did.
type BackfillResult struct {
	Total    int
	Enqueued int
	Skipped  int
}

// Backfill replays every existing project through the outbox as a create event,
// so projects that pre-date the mirror get synced to Tempo. It is idempotent: a
// project that already has any outbox event is skipped, so running the backfill
// twice never double-enqueues. (Tempo's intake is additionally idempotent on the
// project id, so even a duplicate delivery would not double-create.)
func Backfill(ctx context.Context, s BackfillStore, logger *slog.Logger) (BackfillResult, error) {
	if logger == nil {
		logger = slog.Default()
	}
	projects, err := s.ListProjectsWithTenant(ctx)
	if err != nil {
		return BackfillResult{}, fmt.Errorf("list projects: %w", err)
	}
	res := BackfillResult{Total: len(projects)}
	for _, p := range projects {
		existing, err := s.CountProjectSyncEventsByProject(ctx, p.ID)
		if err != nil {
			return res, fmt.Errorf("count events for project %s: %w", p.Slug, err)
		}
		if existing > 0 {
			res.Skipped++
			continue
		}
		ev := NewEvent(EventCreate, p.ID, p.Slug, p.Name, p.TenantSlug)
		if err := Enqueue(ctx, s, ev); err != nil {
			return res, fmt.Errorf("enqueue backfill event for project %s: %w", p.Slug, err)
		}
		res.Enqueued++
		logger.Info("backfilled project", "projectId", p.ID, "slug", p.Slug)
	}
	return res, nil
}
