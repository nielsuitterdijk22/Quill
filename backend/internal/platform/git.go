package platform

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/nielsuitterdijk22/quill/internal/forgejo"
	"github.com/nielsuitterdijk22/quill/internal/store/db"
)

// GitCredential is a short-lived git-over-HTTP credential for the acting user:
// their git username and a freshly minted personal access token. The token is
// shown once and used as the password when cloning or pushing over HTTPS.
type GitCredential struct {
	ID       uuid.UUID
	Username string
	Token    string
}

// GitTokenInfo is the metadata Quill keeps about an outstanding git token so the
// user can see and revoke it. The secret itself is never stored.
type GitTokenInfo struct {
	ID        uuid.UUID
	Name      string
	CreatedAt time.Time
}

// CreateGitToken mints a personal git access token for the actor so they can
// clone and push over HTTPS. Quill provisions Forgejo accounts with an unused
// random password, so it installs a fresh short-lived password via the admin
// API, authenticates as the user to create a scoped token, then discards the
// password. The returned token secret is the only credential the user keeps;
// Quill records just the token's metadata so it can be listed and revoked later.
func (s *Service) CreateGitToken(ctx context.Context, actor Actor, name string) (GitCredential, error) {
	if !s.forgejoEnabled() {
		return GitCredential{}, fmt.Errorf("%w: git access requires the Forgejo backend", ErrUnavailable)
	}
	user, err := s.store.GetUserByID(ctx, actor.UserID)
	if err != nil {
		return GitCredential{}, fmt.Errorf("load user: %w", err)
	}
	if !user.ForgejoUsername.Valid || user.ForgejoUsername.String == "" {
		return GitCredential{}, fmt.Errorf("%w: your account is not yet linked to the git backend", ErrUnavailable)
	}
	username := user.ForgejoUsername.String

	label := strings.TrimSpace(name)
	if label == "" {
		label = "git access token"
	}
	if len(label) > 100 {
		label = label[:100]
	}

	password, err := randomToken(24)
	if err != nil {
		return GitCredential{}, err
	}
	if err := s.forgejo.SetUserPassword(ctx, username, password); err != nil {
		return GitCredential{}, fmt.Errorf("prepare git credential: %w", err)
	}
	forgejoName := fmt.Sprintf("quill-git-%d", time.Now().UnixNano())
	token, err := s.forgejo.CreateAccessToken(ctx, username, password, forgejoName, []string{"write:repository"})
	if err != nil {
		return GitCredential{}, fmt.Errorf("mint git token: %w", err)
	}

	// Record the token so it can be listed and revoked. If recording fails, roll
	// back the Forgejo token so we don't leave an unrevocable credential behind.
	record, err := s.store.CreateGitToken(ctx, db.CreateGitTokenParams{
		UserID:           actor.UserID,
		Name:             label,
		ForgejoTokenName: forgejoName,
	})
	if err != nil {
		cctx, cancel := detachedContext(ctx)
		defer cancel()
		if delErr := s.forgejo.DeleteAccessToken(cctx, username, password, forgejoName); delErr != nil {
			s.logger.Error("failed to roll back git token", "user", username, "token", forgejoName, "error", delErr)
		}
		return GitCredential{}, fmt.Errorf("record git token: %w", err)
	}

	return GitCredential{ID: record.ID, Username: username, Token: token}, nil
}

// ListGitTokens returns the actor's outstanding git tokens (metadata only).
func (s *Service) ListGitTokens(ctx context.Context, actor Actor) ([]GitTokenInfo, error) {
	rows, err := s.store.ListGitTokensByUser(ctx, actor.UserID)
	if err != nil {
		return nil, fmt.Errorf("list git tokens: %w", err)
	}
	out := make([]GitTokenInfo, 0, len(rows))
	for _, r := range rows {
		out = append(out, GitTokenInfo{ID: r.ID, Name: r.Name, CreatedAt: r.CreatedAt})
	}
	return out, nil
}

// RevokeGitToken revokes one of the actor's git tokens: it deletes the token in
// Forgejo (re-installing a short-lived password to authenticate as the user, as
// minting does) and removes Quill's record. A token already gone from Forgejo is
// treated as success so the record can still be cleared.
func (s *Service) RevokeGitToken(ctx context.Context, actor Actor, id uuid.UUID) error {
	if !s.forgejoEnabled() {
		return fmt.Errorf("%w: git access requires the Forgejo backend", ErrUnavailable)
	}
	record, err := s.store.GetGitToken(ctx, db.GetGitTokenParams{ID: id, UserID: actor.UserID})
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("load git token: %w", err)
	}

	user, err := s.store.GetUserByID(ctx, actor.UserID)
	if err != nil {
		return fmt.Errorf("load user: %w", err)
	}
	if !user.ForgejoUsername.Valid || user.ForgejoUsername.String == "" {
		return fmt.Errorf("%w: your account is not yet linked to the git backend", ErrUnavailable)
	}
	username := user.ForgejoUsername.String

	password, err := randomToken(24)
	if err != nil {
		return err
	}
	if err := s.forgejo.SetUserPassword(ctx, username, password); err != nil {
		return fmt.Errorf("prepare git credential: %w", err)
	}
	if err := s.forgejo.DeleteAccessToken(ctx, username, password, record.ForgejoTokenName); err != nil && !forgejo.NotFound(err) {
		return fmt.Errorf("revoke git token: %w", err)
	}
	if err := s.store.DeleteGitToken(ctx, db.DeleteGitTokenParams{ID: id, UserID: actor.UserID}); err != nil {
		return fmt.Errorf("delete git token: %w", err)
	}
	return nil
}

// randomToken returns n cryptographically random bytes hex-encoded.
func randomToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate secret: %w", err)
	}
	return hex.EncodeToString(b), nil
}
