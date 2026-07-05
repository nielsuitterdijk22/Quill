// Package projectsync mirrors Quill project identity to Tempo.
//
// Quill owns Project (id, slug, name, tenant). Tempo needs a fast,
// always-available local copy for its hot path (backlog filters, board
// grouping, permission checks), so Quill *pushes* a mirror instead of Tempo
// reading Quill live. This package implements the push side: a transactional
// outbox (rows enqueued in the same transaction as the project mutation) plus a
// background dispatcher that delivers undelivered events to Tempo's intake
// endpoint with retry and exponential backoff, so events survive Tempo
// downtime.
//
// The event carries only what Quill actually owns — id, slug, name, tenant
// slug, and archived/deleted flags. Key and visibility are deliberately absent:
// Quill has no such columns; Tempo derives them on intake (see the "What Tempo
// derives on intake" section of the tight-Quill-integration design doc).
package projectsync

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/nielsuitterdijk22/quill/internal/store/db"
)

// EventType enumerates the project lifecycle changes mirrored to Tempo.
type EventType string

const (
	// EventCreate is emitted when a project is provisioned (and by the backfill
	// for every pre-existing project).
	EventCreate EventType = "create"
	// EventRename is emitted when a project's slug or name changes. Quill has no
	// project-rename operation today, but the type is defined so the dispatcher
	// handles it unchanged if one is added.
	EventRename EventType = "rename"
	// EventArchive is emitted when a project is archived. Quill has no
	// project-archive operation today; defined for the same forward-compat reason.
	EventArchive EventType = "archive"
	// EventDelete is emitted when a project is deleted. Tempo archives (never
	// cascade-deletes) the mirrored project in response.
	EventDelete EventType = "delete"
)

// Event is the JSON body delivered to Tempo's intake endpoint and the payload
// persisted in the outbox row. EventID and OccurredAt let the Tempo intake
// dedupe under outbox retries and backfill replays; they equal the outbox row's
// id and occurred_at.
type Event struct {
	EventID    uuid.UUID `json:"eventId"`
	EventType  EventType `json:"eventType"`
	OccurredAt time.Time `json:"occurredAt"`

	ProjectID  uuid.UUID `json:"projectId"`
	Slug       string    `json:"slug"`
	Name       string    `json:"name"`
	TenantSlug string    `json:"tenantSlug"`
	// Archived and Deleted are mutually-exclusive lifecycle flags. Both false for
	// a create/rename.
	Archived bool `json:"archived"`
	Deleted  bool `json:"deleted"`
}

// NewEvent builds an Event for a project mutation, assigning a fresh event id
// and stamping the current time. archived/deleted are set from typ.
func NewEvent(typ EventType, projectID uuid.UUID, slug, name, tenantSlug string) Event {
	return Event{
		EventID:    uuid.New(),
		EventType:  typ,
		OccurredAt: time.Now().UTC(),
		ProjectID:  projectID,
		Slug:       slug,
		Name:       name,
		TenantSlug: tenantSlug,
		Archived:   typ == EventArchive,
		Deleted:    typ == EventDelete,
	}
}

// EventWriter is the subset of the store needed to enqueue an outbox row. Both
// *store.Store and a transaction-scoped *db.Queries satisfy it, so callers can
// enqueue inside the SAME transaction as the project mutation.
type EventWriter interface {
	InsertProjectSyncEvent(ctx context.Context, arg db.InsertProjectSyncEventParams) (db.ProjectSyncOutbox, error)
}

// Enqueue persists ev as an outbox row. The row id and occurred_at are taken
// from ev so the stored payload, the row, and what Tempo dedupes on all agree.
// Call this inside the transaction that performs the project mutation: if that
// transaction rolls back, no event is emitted.
func Enqueue(ctx context.Context, w EventWriter, ev Event) error {
	payload, err := json.Marshal(ev)
	if err != nil {
		return fmt.Errorf("marshal project sync event: %w", err)
	}
	_, err = w.InsertProjectSyncEvent(ctx, db.InsertProjectSyncEventParams{
		ID:         ev.EventID,
		ProjectID:  ev.ProjectID,
		EventType:  string(ev.EventType),
		Payload:    payload,
		OccurredAt: ev.OccurredAt,
	})
	if err != nil {
		return fmt.Errorf("enqueue project sync event: %w", err)
	}
	return nil
}
