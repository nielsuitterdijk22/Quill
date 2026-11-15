package platform

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/nielsuitterdijk22/quill/internal/auth"
)

// API tokens (2026-11-15): a bearer credential for the REST API, so
// automation (agents opening/merging PRs, triggering runs) works without an
// interactive Zitadel sign-in. They act as their user, like a session.
//
// They live beside the native git tokens (same table, same list, same
// revoke) and differ by prefix: only the SHA-256 of the full secret is stored
// and lookups hash what was presented, so a git token (qgt_) is never an API
// credential and an API token (qat_) never authenticates git. No schema change.
const APITokenPrefix = "qat_"

// apiTokenLabelPrefix marks API tokens in the user's token list.
const apiTokenLabelPrefix = "API · "

// ErrAPITokenInvalid means the presented API token is unknown or revoked, or
// its user is inactive.
var ErrAPITokenInvalid = errors.New("invalid api token")

// MintAPIToken creates an API token for the actor. The secret is returned
// once; only its hash is stored.
func (s *Service) MintAPIToken(ctx context.Context, actor Actor, name string) (GitCredential, error) {
	user, err := s.store.GetUserByID(ctx, actor.UserID)
	if err != nil {
		return GitCredential{}, fmt.Errorf("load user: %w", err)
	}
	label := strings.TrimSpace(name)
	if label == "" {
		label = "token"
	}
	if len(label) > 90 {
		label = label[:90]
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return GitCredential{}, err
	}
	secret := APITokenPrefix + base64.RawURLEncoding.EncodeToString(raw)
	sum := sha256.Sum256([]byte(secret))
	id, err := s.store.CreateNativeGitToken(ctx, actor.UserID, apiTokenLabelPrefix+label, hex.EncodeToString(sum[:]))
	if err != nil {
		return GitCredential{}, err
	}
	return GitCredential{ID: id, Username: user.Username, Token: secret}, nil
}

// AuthenticateAPIToken resolves an API token to its (active) user.
func (s *Service) AuthenticateAPIToken(ctx context.Context, token string) (auth.Identity, error) {
	if !strings.HasPrefix(token, APITokenPrefix) {
		return auth.Identity{}, ErrAPITokenInvalid
	}
	sum := sha256.Sum256([]byte(token))
	_, userID, err := s.store.GitTokenByHash(ctx, hex.EncodeToString(sum[:]))
	if errors.Is(err, pgx.ErrNoRows) {
		return auth.Identity{}, ErrAPITokenInvalid
	}
	if err != nil {
		return auth.Identity{}, err
	}
	user, err := s.store.GetUserByID(ctx, userID)
	if err != nil || !user.IsActive {
		return auth.Identity{}, ErrAPITokenInvalid
	}
	id := auth.Identity{UserID: user.ID, Username: user.Username, Email: user.Email, IsAdmin: user.IsAdmin}
	if user.TenantID.Valid {
		id.TenantID = user.TenantID.UUID
	}
	return id, nil
}
