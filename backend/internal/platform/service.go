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
// admins bypass org membership checks; everyone else must belong to the org they
// act on.
type Actor struct {
	UserID  uuid.UUID
	IsAdmin bool
}

// Service implements org and repository management on top of the store and the
// Forgejo client. The Forgejo client may be disabled (see forgejo.Client.Enabled),
// in which case Quill records metadata only and skips git-side provisioning so
// local development works without a running Forgejo.
type Service struct {
	store   *store.Store
	forgejo *forgejo.Client
	logger  *slog.Logger
	// runner executes CI workflows. It is the seam behind which nektos/act (today)
	// or Forge's ephemeral runners (later) sit; see internal/pipeline.
	runner pipeline.Runner
}

// NewService wires a platform Service. logger may be nil. The CI runner defaults
// to the nektos/act-backed runner; override it with WithRunner in tests.
func NewService(st *store.Store, fj *forgejo.Client, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{store: st, forgejo: fj, logger: logger, runner: pipeline.NewActRunner()}
}

// WithRunner overrides the CI runner (used by tests and to swap in a future
// forgeRunner) and returns the service for chaining.
func (s *Service) WithRunner(r pipeline.Runner) *Service {
	s.runner = r
	return s
}

// forgejoEnabled reports whether git-side provisioning is active.
func (s *Service) forgejoEnabled() bool {
	return s.forgejo != nil && s.forgejo.Enabled()
}

// authorizeOrgMember returns nil when the actor is a platform admin or a member
// of orgID, and ErrForbidden otherwise. Membership is read from the org roster;
// rosters are small (org owners/maintainers), so a full read is acceptable here.
func (s *Service) authorizeOrgMember(ctx context.Context, actor Actor, orgID uuid.UUID) error {
	if actor.IsAdmin {
		return nil
	}
	members, err := s.store.ListOrgMembers(ctx, orgID)
	if err != nil {
		return fmt.Errorf("check org membership: %w", err)
	}
	for _, m := range members {
		if m.ID == actor.UserID {
			return nil
		}
	}
	return ErrForbidden
}

// authorizeOrgAdmin returns nil when the actor is a platform admin or an owner
// of orgID, and ErrForbidden otherwise. It gates configuration changes (such as
// editing branch policies) that ordinary members may not perform.
func (s *Service) authorizeOrgAdmin(ctx context.Context, actor Actor, orgID uuid.UUID) error {
	if actor.IsAdmin {
		return nil
	}
	members, err := s.store.ListOrgMembers(ctx, orgID)
	if err != nil {
		return fmt.Errorf("check org membership: %w", err)
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
