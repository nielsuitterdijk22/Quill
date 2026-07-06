package workitemrefs

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/nielsuitterdijk22/quill/internal/outbox"
	"github.com/nielsuitterdijk22/quill/internal/store/db"
)

// TokenSource and StaticTokenSource are the shared auth seam from internal/outbox
// (also re-exported by projectsync); both outbound pushes present their bearer
// token through the same interface.
type TokenSource = outbox.TokenSource
type StaticTokenSource = outbox.StaticTokenSource

// Config and Dispatcher are the shared outbox engine's types. Work-item-ref
// linking is one concrete outbox over that engine.
type Config = outbox.Config
type Dispatcher = outbox.Dispatcher

// Outbox is the generated-query surface for the work_item_ref_outbox table.
// *store.Store satisfies it via its embedded *db.Queries; tests supply a fake.
type Outbox interface {
	ListPendingWorkItemRefEvents(ctx context.Context, limit int32) ([]db.WorkItemRefOutbox, error)
	MarkWorkItemRefEventDelivered(ctx context.Context, id uuid.UUID) error
	MarkWorkItemRefEventFailed(ctx context.Context, arg db.MarkWorkItemRefEventFailedParams) error
}

// storeAdapter maps the work_item_ref_outbox queries onto the generic
// outbox.Store the shared dispatcher drives.
type storeAdapter struct{ q Outbox }

func (a storeAdapter) ListPending(ctx context.Context, limit int32) ([]outbox.PendingEvent, error) {
	rows, err := a.q.ListPendingWorkItemRefEvents(ctx, limit)
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
	return a.q.MarkWorkItemRefEventDelivered(ctx, id)
}

func (a storeAdapter) MarkFailed(ctx context.Context, id uuid.UUID, nextAttemptAt time.Time) error {
	return a.q.MarkWorkItemRefEventFailed(ctx, db.MarkWorkItemRefEventFailedParams{
		ID:            id,
		NextAttemptAt: nextAttemptAt,
	})
}

// NewDispatcher builds the work-item-ref dispatcher over the shared outbox
// engine. The signature is unchanged, so server wiring and tests are untouched.
func NewDispatcher(cfg Config, ob Outbox, tokens TokenSource, logger *slog.Logger) *Dispatcher {
	if cfg.Name == "" {
		cfg.Name = "work-item ref sync"
	}
	return outbox.NewDispatcher(cfg, storeAdapter{q: ob}, tokens, logger)
}
