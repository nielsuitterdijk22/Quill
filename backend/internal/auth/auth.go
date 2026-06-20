// Package auth provides Quill's authentication layer.
//
// Authentication is deliberately kept behind the Provider interface so the local
// username/password provider used today can be swapped for, or joined by, OIDC
// providers (Keycloak, Entra, GitHub) later without touching callers. The HTTP
// layer and Service depend only on Provider, Identity, and the token service.
package auth

import (
	"context"
	"errors"

	"github.com/google/uuid"
)

// Provider names. Stored verbatim in auth_identities.provider.
const (
	ProviderLocal = "local"
	ProviderClerk = "clerk"
)

// Identity is the authenticated Quill principal returned by a Provider and
// embedded (in part) in issued tokens.
type Identity struct {
	UserID   uuid.UUID
	Username string
	Email    string
	IsAdmin  bool
	TenantID uuid.UUID
}

// Credentials carries provider-specific authentication input. The local provider
// reads Username and Password; token-based providers added later (OIDC) will read
// Token. A provider ignores fields it does not use.
type Credentials struct {
	Username string
	Password string
	Token    string
}

// Sentinel errors. Authenticate must return ErrInvalidCredentials for any
// authentication failure (unknown subject, bad password, inactive user) so the
// HTTP layer can return a single, non-enumerating 401.
var (
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrUserExists         = errors.New("user already exists")
	ErrInvalidInput       = errors.New("invalid input")
)

// Provider authenticates Credentials against a backing identity source and maps
// the result to a Quill Identity. Implementations must be safe for concurrent use.
type Provider interface {
	// Name returns the provider key stored in auth_identities.provider.
	Name() string
	// Authenticate verifies the credentials and returns the mapped identity, or
	// ErrInvalidCredentials on any failure.
	Authenticate(ctx context.Context, c Credentials) (Identity, error)
}
