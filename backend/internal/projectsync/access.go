package projectsync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/nielsuitterdijk22/quill/internal/store/db"
)

// EventAccess tells Tempo that who can reach a project changed. Unlike the
// lifecycle events, its payload is not stored when the change happens: database
// triggers queue an empty row, and the dispatcher builds the snapshot from the
// data as it is at delivery time (see storeAdapter.ListPending), so a retried
// event never delivers a stale roster.
const EventAccess EventType = "access"

// AccessMember is a person on the project's tenant roster.
type AccessMember struct {
	UserID      uuid.UUID `json:"userId"`
	Username    string    `json:"username"`
	Email       string    `json:"email"`
	DisplayName string    `json:"displayName"`
	// TenantRole is "owner" (an org admin) or "member".
	TenantRole string `json:"tenantRole"`
}

// AccessGrant is a person's resolved role on the project (project_access(): the
// strongest of their direct and group grants). People with no access are on the
// roster but have no grant. Org admins have no standing access.
type AccessGrant struct {
	UserID uuid.UUID `json:"userId"`
	Role   string    `json:"role"` // owner | member | read
}

// AccessEvent is the JSON body delivered to Tempo for an 'access' event: a full
// snapshot, so Tempo replaces its roster and replays are harmless.
type AccessEvent struct {
	EventID    uuid.UUID      `json:"eventId"`
	EventType  EventType      `json:"eventType"`
	OccurredAt time.Time      `json:"occurredAt"`
	ProjectID  uuid.UUID      `json:"projectId"`
	Members    []AccessMember `json:"members"`
	Grants     []AccessGrant  `json:"grants"`
}

// SnapshotReader is the store surface the snapshot needs. *store.Store satisfies it.
type SnapshotReader interface {
	GetProjectByID(ctx context.Context, id uuid.UUID) (db.Project, error)
	ListProjectAccessSnapshot(ctx context.Context, projectID uuid.UUID) ([]db.ListProjectAccessSnapshotRow, error)
}

// BuildAccessEvent reads the project's current access. found is false when the
// project no longer exists, in which case there is nothing to mirror.
func BuildAccessEvent(ctx context.Context, r SnapshotReader, eventID, projectID uuid.UUID, occurredAt time.Time) (AccessEvent, bool, error) {
	if _, err := r.GetProjectByID(ctx, projectID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return AccessEvent{}, false, nil
		}
		return AccessEvent{}, false, fmt.Errorf("load project: %w", err)
	}
	rows, err := r.ListProjectAccessSnapshot(ctx, projectID)
	if err != nil {
		return AccessEvent{}, false, fmt.Errorf("read access snapshot: %w", err)
	}
	ev := AccessEvent{
		EventID: eventID, EventType: EventAccess, OccurredAt: occurredAt, ProjectID: projectID,
		Members: make([]AccessMember, 0, len(rows)), Grants: make([]AccessGrant, 0, len(rows)),
	}
	for _, row := range rows {
		ev.Members = append(ev.Members, AccessMember{
			UserID: row.UserID, Username: row.Username, Email: row.Email, DisplayName: row.DisplayName, TenantRole: row.TenantRole,
		})
		if row.Role != "" {
			ev.Grants = append(ev.Grants, AccessGrant{UserID: row.UserID, Role: row.Role})
		}
	}
	return ev, true, nil
}

func marshalAccessEvent(ev AccessEvent) ([]byte, error) { return json.Marshal(ev) }
