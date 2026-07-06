package projectsync

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/nielsuitterdijk22/quill/internal/outbox"
	"github.com/nielsuitterdijk22/quill/internal/store/db"
)

// TokenSource and StaticTokenSource are re-exported from internal/outbox so the
// existing projectsync.TokenSource seam (SelectTempoTokenSource, the server
// wiring, the work-item-ref dispatcher) keeps referring through one type.
type TokenSource = outbox.TokenSource
type StaticTokenSource = outbox.StaticTokenSource

// Config and Dispatcher are the shared outbox engine's types. Project mirroring
// is one concrete outbox over that engine; the poll/backoff/POST loop lives in
// internal/outbox, not here.
type Config = outbox.Config
type Dispatcher = outbox.Dispatcher

// Outbox is the generated-query surface for the project_sync_outbox table.
// *store.Store satisfies it via its embedded *db.Queries; tests supply a fake.
type Outbox interface {
	ListPendingProjectSyncEvents(ctx context.Context, limit int32) ([]db.ProjectSyncOutbox, error)
	MarkProjectSyncEventDelivered(ctx context.Context, id uuid.UUID) error
	MarkProjectSyncEventFailed(ctx context.Context, arg db.MarkProjectSyncEventFailedParams) error
}

// storeAdapter maps the project_sync_outbox queries onto the generic
// outbox.Store the shared dispatcher drives.
type storeAdapter struct{ q Outbox }

func (a storeAdapter) ListPending(ctx context.Context, limit int32) ([]outbox.PendingEvent, error) {
	rows, err := a.q.ListPendingProjectSyncEvents(ctx, limit)
	if err != nil {
		return nil, err
	}
	events := make([]outbox.PendingEvent, len(rows))
	for i, r := range rows {
		events[i] = outbox.PendingEvent{ID: r.ID, Payload: r.Payload, Attempts: r.Attempts}
	}
	return events, nil
}

func (a storeAdapter) MarkDelivered(ctx context.Context, id uuid.UUID) error {
	return a.q.MarkProjectSyncEventDelivered(ctx, id)
}

func (a storeAdapter) MarkFailed(ctx context.Context, id uuid.UUID, nextAttemptAt time.Time) error {
	return a.q.MarkProjectSyncEventFailed(ctx, db.MarkProjectSyncEventFailedParams{
		ID:            id,
		NextAttemptAt: nextAttemptAt,
	})
}

// NewDispatcher builds the project-mirror dispatcher over the shared outbox
// engine. The signature is unchanged, so server wiring and tests are untouched.
func NewDispatcher(cfg Config, ob Outbox, tokens TokenSource, logger *slog.Logger) *Dispatcher {
	if cfg.Name == "" {
		cfg.Name = "project sync"
	}
	return outbox.NewDispatcher(cfg, storeAdapter{q: ob}, tokens, logger)
}
