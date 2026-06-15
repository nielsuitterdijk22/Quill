package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/mail"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"golang.org/x/crypto/bcrypt"

	"github.com/nielsuitterdijk22/quill/internal/forgejo"
	"github.com/nielsuitterdijk22/quill/internal/store"
	"github.com/nielsuitterdijk22/quill/internal/store/db"
)

const (
	minPasswordLen = 8
	// bcrypt silently truncates input beyond 72 bytes, so reject longer secrets
	// rather than hash a prefix.
	maxPasswordLen = 72
)

var usernameRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{1,38}$`)

// RegisterInput is the payload for creating a local account.
type RegisterInput struct {
	Username    string
	Email       string
	DisplayName string
	Password    string
}

// Service coordinates the auth provider, token issuance, and the user store. The
// HTTP layer talks only to Service; provider selection lives here so OIDC can be
// added without changing handlers.
type Service struct {
	store    *store.Store
	provider Provider
	tokens   *TokenService
	forgejo  *forgejo.Client
	logger   *slog.Logger
}

// NewService wires a Service. provider is the credential authenticator (local
// today); tokens issues/verifies access tokens.
func NewService(st *store.Store, provider Provider, tokens *TokenService) *Service {
	return &Service{store: st, provider: provider, tokens: tokens, logger: slog.Default()}
}

// WithForgejo enables best-effort Forgejo user provisioning on registration: a
// matching Forgejo account is created and linked to the Quill user. Forgejo
// failures are logged but never block registration, since Quill — not Forgejo —
// owns authentication. Returns the same Service for chaining.
func (s *Service) WithForgejo(fj *forgejo.Client, logger *slog.Logger) *Service {
	s.forgejo = fj
	if logger != nil {
		s.logger = logger
	}
	return s
}

// Tokens exposes the token service for the HTTP middleware.
func (s *Service) Tokens() *TokenService { return s.tokens }

// Register creates a new local user and its bcrypt-backed auth identity in one
// transaction. The very first user created becomes an admin so a fresh install
// has an operator. Returns the new Identity on success.
func (s *Service) Register(ctx context.Context, in RegisterInput) (Identity, error) {
	username := strings.TrimSpace(in.Username)
	email := strings.TrimSpace(in.Email)
	display := strings.TrimSpace(in.DisplayName)

	if !usernameRe.MatchString(username) {
		return Identity{}, fmt.Errorf("%w: username must be 2-39 chars of letters, digits, '-' or '_' and start alphanumeric", ErrInvalidInput)
	}
	if _, err := mail.ParseAddress(email); err != nil {
		return Identity{}, fmt.Errorf("%w: email is not valid", ErrInvalidInput)
	}
	if len(in.Password) < minPasswordLen {
		return Identity{}, fmt.Errorf("%w: password must be at least %d characters", ErrInvalidInput, minPasswordLen)
	}
	if len(in.Password) > maxPasswordLen {
		return Identity{}, fmt.Errorf("%w: password must be at most %d characters", ErrInvalidInput, maxPasswordLen)
	}
	if display == "" {
		display = username
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(in.Password), bcrypt.DefaultCost)
	if err != nil {
		return Identity{}, fmt.Errorf("hash password: %w", err)
	}

	var id Identity
	err = s.store.InTx(ctx, func(q *db.Queries) error {
		count, err := q.CountUsers(ctx)
		if err != nil {
			return fmt.Errorf("count users: %w", err)
		}
		user, err := q.CreateUser(ctx, db.CreateUserParams{
			Username:    username,
			Email:       email,
			DisplayName: display,
			IsAdmin:     count == 0,
			IsActive:    true,
		})
		if err != nil {
			return err
		}
		if _, err := q.CreateAuthIdentity(ctx, db.CreateAuthIdentityParams{
			UserID:     user.ID,
			Provider:   ProviderLocal,
			Subject:    strings.ToLower(username),
			SecretHash: pgtype.Text{String: string(hash), Valid: true},
		}); err != nil {
			return err
		}
		id = Identity{UserID: user.ID, Username: user.Username, Email: user.Email, IsAdmin: user.IsAdmin}
		return nil
	})
	if err != nil {
		if isUniqueViolation(err) {
			return Identity{}, ErrUserExists
		}
		return Identity{}, err
	}

	// Provision the Forgejo account off the request path: the Quill user already
	// exists, and a slow or hung Forgejo must not add latency to registration.
	if s.forgejo != nil && s.forgejo.Enabled() {
		go s.provisionForgejoUser(context.WithoutCancel(ctx), id)
	}
	return id, nil
}

// provisionForgejoUser best-effort creates a Forgejo account for a freshly
// registered Quill user and records the linkage. Quill mediates all Forgejo
// access via the admin token, so the Forgejo password is random and unused. Any
// failure is logged and swallowed: a missing Forgejo link can be repaired later
// and must not prevent the user from signing in.
func (s *Service) provisionForgejoUser(ctx context.Context, id Identity) {
	if s.forgejo == nil || !s.forgejo.Enabled() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	fjUser, err := s.forgejo.CreateUser(ctx, forgejo.CreateUserOptions{
		Username:           id.Username,
		Email:              id.Email,
		Password:           randomSecret(),
		MustChangePassword: false,
	})
	if err != nil {
		// The account may already exist (e.g. a retry); fall back to a lookup so
		// we can still establish the link.
		existing, getErr := s.forgejo.GetUser(ctx, id.Username)
		if getErr != nil {
			s.logger.Error("forgejo user provisioning failed", "username", id.Username, "error", err)
			return
		}
		fjUser = existing
	}

	if _, err := s.store.SetUserForgejoLink(ctx, db.SetUserForgejoLinkParams{
		ID:              id.UserID,
		ForgejoUserID:   pgtype.Int8{Int64: fjUser.ID, Valid: true},
		ForgejoUsername: pgtype.Text{String: fjUser.Login, Valid: true},
	}); err != nil {
		s.logger.Error("failed to link forgejo user", "username", id.Username, "error", err)
	}
}

// randomSecret returns a 32-byte hex string for the Forgejo-side password. It
// falls back to a fixed-length constant only if the system RNG fails, which is
// acceptable because the value is never used to authenticate.
func randomSecret() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "quill-forgejo-unused-secret-fallback"
	}
	return hex.EncodeToString(b)
}

// Login authenticates credentials via the provider and issues a token. It returns
// the signed token, its expiry, and the authenticated Identity.
func (s *Service) Login(ctx context.Context, c Credentials) (string, Identity, error) {
	id, err := s.provider.Authenticate(ctx, c)
	if err != nil {
		return "", Identity{}, err
	}
	token, _, err := s.tokens.Issue(id)
	if err != nil {
		return "", Identity{}, err
	}
	return token, id, nil
}

// CurrentUser loads the full user record for an authenticated identity.
func (s *Service) CurrentUser(ctx context.Context, id Identity) (db.User, error) {
	return s.store.GetUserByID(ctx, id.UserID)
}

// maxDisplayNameLen bounds a display name so profiles stay sane in the UI.
const maxDisplayNameLen = 100

// UpdateProfile updates the signed-in user's editable profile fields (currently
// just the display name) and returns the refreshed record. An empty display name
// is allowed and clears it; the UI then falls back to the username.
func (s *Service) UpdateProfile(ctx context.Context, id Identity, displayName string) (db.User, error) {
	displayName = strings.TrimSpace(displayName)
	if len(displayName) > maxDisplayNameLen {
		return db.User{}, fmt.Errorf("%w: display name must be at most %d characters", ErrInvalidInput, maxDisplayNameLen)
	}
	return s.store.UpdateUserProfile(ctx, db.UpdateUserProfileParams{
		ID:          id.UserID,
		DisplayName: displayName,
	})
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
