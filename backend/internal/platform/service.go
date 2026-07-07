package platform

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nielsuitterdijk22/quill/internal/forgejo"
	"github.com/nielsuitterdijk22/quill/internal/pipeline"
	"github.com/nielsuitterdijk22/quill/internal/policy"
	"github.com/nielsuitterdijk22/quill/internal/secretbox"
	"github.com/nielsuitterdijk22/quill/internal/store"
	"github.com/nielsuitterdijk22/quill/internal/store/db"
)

// Actor is the authenticated principal performing a platform operation. Platform
// admins bypass project membership checks; everyone else must belong to the
// project they act on.
type Actor struct {
	UserID   uuid.UUID
	IsAdmin  bool
	TenantID uuid.UUID
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
	// evaluator turns the policies governing a gate plus typed facts into a
	// verdict (unanimous-allow across scopes). The typed evaluator is the hot
	// path: no per-request Rego compile. The embedded-OPA evaluator stays as the
	// parity oracle and can be swapped in via WithEvaluator.
	evaluator policy.Evaluator
	// activeRuns maps run UUID strings to their live log broadcaster. Entries
	// are added when a pipeline run starts and removed a few minutes after it
	// finishes so late SSE subscribers can still replay buffered output.
	activeRuns sync.Map // string (run UUID) → *logBroadcaster
	// cipher encrypts pipeline secrets at rest. NewService installs the insecure
	// development cipher; production wiring overrides it via WithCipher with the
	// key from QUILL_SECRET_ENCRYPTION_KEY.
	cipher *secretbox.Cipher
}

// NewService wires a platform Service. logger may be nil. The CI runner defaults
// to the nektos/act-backed runner; production wiring can override it with the
// HTTP dispatcher via WithRunner. The policy evaluator defaults to the typed
// evaluator (override with WithEvaluator).
func NewService(st *store.Store, fj *forgejo.Client, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{
		store:     st,
		forgejo:   fj,
		logger:    logger,
		runner:    pipeline.NewActRunner(),
		evaluator: policy.NewTypedEvaluator(),
		cipher:    secretbox.NewDev(),
	}
}

// WithRunner overrides the CI runner (used by tests and the HTTP dispatcher
// client) and returns the service for chaining.
func (s *Service) WithRunner(r pipeline.Runner) *Service {
	s.runner = r
	return s
}

// WithCipher overrides the pipeline-secret cipher (used by production wiring to
// install the configured encryption key) and returns the service for chaining.
func (s *Service) WithCipher(c *secretbox.Cipher) *Service {
	s.cipher = c
	return s
}

// WithEvaluator overrides the policy evaluator (used by tests and to swap in the
// embedded-OPA evaluator) and returns the service for chaining.
func (s *Service) WithEvaluator(e policy.Evaluator) *Service {
	s.evaluator = e
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

// authorizePlatformAdmin returns nil when the actor is a platform admin and
// ErrForbidden otherwise. It gates tenant-scoped configuration (such as tenant
// branch policies), which is governance reserved for platform operators.
func (s *Service) authorizePlatformAdmin(actor Actor) error {
	if actor.IsAdmin {
		return nil
	}
	return ErrForbidden
}

// authorizeTenantMember returns nil when the actor is a platform admin or
// belongs to tenantID, and ErrForbidden otherwise. It gates read-only access to
// tenant-scoped governance: every member of a tenant may view the policies that
// apply to them, while only platform admins may change them
// (authorizePlatformAdmin).
func (s *Service) authorizeTenantMember(actor Actor, tenantID uuid.UUID) error {
	if actor.IsAdmin {
		return nil
	}
	if actor.TenantID == tenantID {
		return nil
	}
	return ErrForbidden
}

// provisionForgejoUser creates a Forgejo account for user and writes the link
// back to the database. It is idempotent: if the Forgejo account already exists
// the existing one is used. This is called on-demand when a user's Forgejo link
// is absent — e.g. because provisioning failed at signup due to a stale token.
func (s *Service) provisionForgejoUser(ctx context.Context, user db.User) error {
	if !s.forgejoEnabled() {
		return fmt.Errorf("Forgejo is not configured")
	}
	fjUser, err := s.forgejo.CreateUser(ctx, forgejo.CreateUserOptions{
		Username:           user.Username,
		Email:              user.Email,
		Password:           func() string { s, _ := randomToken(24); return s }(),
		MustChangePassword: false,
	})
	if err != nil {
		existing, getErr := s.forgejo.GetUser(ctx, user.Username)
		if getErr != nil {
			return fmt.Errorf("create forgejo user: %w", err)
		}
		fjUser = existing
	}
	_, err = s.store.SetUserForgejoLink(ctx, db.SetUserForgejoLinkParams{
		ID:              user.ID,
		ForgejoUserID:   pgtype.Int8{Int64: fjUser.ID, Valid: true},
		ForgejoUsername: pgtype.Text{String: fjUser.Login, Valid: true},
	})
	return err
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
