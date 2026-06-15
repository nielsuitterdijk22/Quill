package platform

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

// GitCredential is a short-lived git-over-HTTP credential for the acting user:
// their git username and a freshly minted personal access token. The token is
// shown once and used as the password when cloning or pushing over HTTPS.
type GitCredential struct {
	Username string
	Token    string
}

// CreateGitToken mints a personal git access token for the actor so they can
// clone and push over HTTPS. Quill provisions Forgejo accounts with an unused
// random password, so it installs a fresh short-lived password via the admin
// API, authenticates as the user to create a scoped token, then discards the
// password. The returned token is the only credential the user keeps.
func (s *Service) CreateGitToken(ctx context.Context, actor Actor) (GitCredential, error) {
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

	password, err := randomToken(24)
	if err != nil {
		return GitCredential{}, err
	}
	if err := s.forgejo.SetUserPassword(ctx, username, password); err != nil {
		return GitCredential{}, fmt.Errorf("prepare git credential: %w", err)
	}
	name := fmt.Sprintf("quill-git-%d", time.Now().UnixNano())
	token, err := s.forgejo.CreateAccessToken(ctx, username, password, name, []string{"write:repository"})
	if err != nil {
		return GitCredential{}, fmt.Errorf("mint git token: %w", err)
	}
	return GitCredential{Username: username, Token: token}, nil
}

// randomToken returns n cryptographically random bytes hex-encoded.
func randomToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate secret: %w", err)
	}
	return hex.EncodeToString(b), nil
}
