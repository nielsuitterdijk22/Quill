package auth

import (
	"context"
	"strings"

	"golang.org/x/crypto/bcrypt"

	"github.com/nielsuitterdijk22/quill/internal/store"
	"github.com/nielsuitterdijk22/quill/internal/store/db"
)

// LocalProvider authenticates against bcrypt password hashes stored in
// auth_identities (provider="local"). The subject is the lower-cased username,
// matching how the Service writes identities on registration.
type LocalProvider struct {
	store *store.Store
}

// NewLocalProvider returns a local username/password provider backed by st.
func NewLocalProvider(st *store.Store) *LocalProvider {
	return &LocalProvider{store: st}
}

// Name implements Provider.
func (p *LocalProvider) Name() string { return ProviderLocal }

// Authenticate verifies a username/password pair. Every failure path returns
// ErrInvalidCredentials so callers can't distinguish "no such user" from "wrong
// password". A bcrypt comparison runs even for unknown users would be ideal to
// equalize timing; here we keep it simple and rely on the single error.
func (p *LocalProvider) Authenticate(ctx context.Context, c Credentials) (Identity, error) {
	username := strings.TrimSpace(c.Username)
	if username == "" || c.Password == "" {
		return Identity{}, ErrInvalidCredentials
	}

	ident, err := p.store.GetAuthIdentity(ctx, db.GetAuthIdentityParams{
		Provider: ProviderLocal,
		Subject:  strings.ToLower(username),
	})
	if err != nil {
		return Identity{}, ErrInvalidCredentials
	}
	if !ident.SecretHash.Valid {
		return Identity{}, ErrInvalidCredentials
	}
	if err := bcrypt.CompareHashAndPassword([]byte(ident.SecretHash.String), []byte(c.Password)); err != nil {
		return Identity{}, ErrInvalidCredentials
	}

	user, err := p.store.GetUserByID(ctx, ident.UserID)
	if err != nil {
		return Identity{}, ErrInvalidCredentials
	}
	if !user.IsActive {
		return Identity{}, ErrInvalidCredentials
	}

	return Identity{
		UserID:   user.ID,
		Username: user.Username,
		Email:    user.Email,
		IsAdmin:  user.IsAdmin,
	}, nil
}

// compile-time check.
var _ Provider = (*LocalProvider)(nil)
