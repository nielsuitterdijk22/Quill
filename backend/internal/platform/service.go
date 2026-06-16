package platform

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/nielsuitterdijk22/quill/internal/forgejo"
	"github.com/nielsuitterdijk22/quill/internal/pipeline"
	"github.com/nielsuitterdijk22/quill/internal/store"
)

// Actor is the authenticated principal performing a platform operation. Platform
// admins bypass project membership checks; everyone else must belong to the
// project they act on.
type Actor struct {
	UserID  uuid.UUID
	IsAdmin bool
}

// Service implements project and repository management on top of the store and
// the Forgejo client. The Forgejo client may be disabled (see
// forgejo.Client.Enabled), in which case Quill records metadata only and skips
// git-side provisioning so local development works without a running Forgejo.
type Service struct {
	store   *store.Store
	forgejo *forgejo.Client
	logger  *slog.Logger
	// runner dispatches CI workflows. In compose this is an HTTP client to the
	// standalone dispatcher; tests can still inject an in-process runner.
	runner pipeline.Runner
}

// NewService wires a platform Service. logger may be nil. The CI runner defaults
// to the nektos/act-backed runner; production wiring can override it with the
// HTTP dispatcher via WithRunner.
func NewService(st *store.Store, fj *forgejo.Client, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{store: st, forgejo: fj, logger: logger, runner: pipeline.NewActRunner()}
}

// WithRunner overrides the CI runner (used by tests and the HTTP dispatcher
// client) and returns the service for chaining.
func (s *Service) WithRunner(r pipeline.Runner) *Service {
	s.runner = r
	return s
}

// forgejoEnabled reports whether git-side provisioning is active.
func (s *Service) forgejoEnabled() bool {
	return s.forgejo != nil && s.forgejo.Enabled()
}

// authorizeProjectMember returns nil when the actor is a platform admin or a
// member of projectID, and ErrForbidden otherwise. Membership is read from the
// project roster; rosters are small, so a full read is acceptable here.
func (s *Service) authorizeProjectMember(ctx context.Context, actor Actor, projectID uuid.UUID) error {
	if actor.IsAdmin {
		return nil
	}
	members, err := s.store.ListProjectMembers(ctx, projectID)
	if err != nil {
		return fmt.Errorf("check project membership: %w", err)
	}
	for _, m := range members {
		if m.ID == actor.UserID {
			return nil
		}
	}
	return ErrForbidden
}

// authorizeProjectAdmin returns nil when the actor is a platform admin or an
// owner of projectID, and ErrForbidden otherwise. It gates configuration changes
// (such as editing branch policies) that ordinary members may not perform.
func (s *Service) authorizeProjectAdmin(ctx context.Context, actor Actor, projectID uuid.UUID) error {
	if actor.IsAdmin {
		return nil
	}
	members, err := s.store.ListProjectMembers(ctx, projectID)
	if err != nil {
		return fmt.Errorf("check project membership: %w", err)
	}
	for _, m := range members {
		if m.ID == actor.UserID {
			if m.MemberRole == "owner" {
				return nil
			}
			return ErrForbidden
		}
	}
	return ErrForbidden
}

// isUniqueViolation reports whether err is a Postgres unique-constraint violation,
// used to translate races into ErrConflict.
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

// detachedContext derives a context that keeps the request's values but is
// decoupled from its cancellation/deadline, with its own bounded timeout. It's
// used for compensating Forgejo cleanup so deletion still runs when the original
// request was cancelled or timed out. Callers must defer the returned cancel.
func detachedContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
}
