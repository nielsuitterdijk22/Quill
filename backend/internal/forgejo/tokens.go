package forgejo

import (
	"context"
	"errors"
	"net/http"
	"net/url"
)

// This file covers the narrow git-credential surface: installing a short-lived
// password for a user (admin) and minting a personal access token as that user.
// Quill mediates all Forgejo access via the admin token, but git-over-HTTP needs
// a credential the user themselves holds, so Quill issues them a scoped token.

// editUserOptions is the admin user-edit payload. Forgejo's admin edit endpoint
// requires LoginName and SourceID even when only the password is changing.
type editUserOptions struct {
	LoginName string `json:"login_name"`
	SourceID  int64  `json:"source_id"`
	Password  string `json:"password,omitempty"`
}

// SetUserPassword sets a user's Forgejo password (admin). Quill installs a random
// short-lived password so it can authenticate as the user to mint a git token;
// the password is then discarded.
func (c *Client) SetUserPassword(ctx context.Context, username, password string) error {
	body := editUserOptions{LoginName: username, SourceID: 0, Password: password}
	return c.do(ctx, http.MethodPatch, "/admin/users/"+url.PathEscape(username), body, nil)
}

// CreateAccessToken mints a personal access token for username using that user's
// basic-auth credentials — Forgejo forbids creating tokens via the admin token.
// It returns the token secret, which is shown to the user once.
func (c *Client) CreateAccessToken(ctx context.Context, username, password, name string, scopes []string) (string, error) {
	body := struct {
		Name   string   `json:"name"`
		Scopes []string `json:"scopes"`
	}{Name: name, Scopes: scopes}
	var out struct {
		SHA1 string `json:"sha1"`
	}
	path := "/users/" + url.PathEscape(username) + "/tokens"
	if err := c.doBasicAuth(ctx, http.MethodPost, path, username, password, body, &out); err != nil {
		return "", err
	}
	return out.SHA1, nil
}

// CloneURL returns an HTTP clone URL for owner/name with the admin token embedded
// as the basic-auth username, suitable for `git clone`. It is used by the CI
// runner to check a repository out into the build workspace. Returns an error
// when Forgejo is disabled (no token) since the URL would not authenticate.
func (c *Client) CloneURL(owner, name string) (string, error) {
	if c.base == "" || c.token == "" {
		return "", errors.New("forgejo client disabled")
	}
	u, err := url.Parse(c.base)
	if err != nil {
		return "", err
	}
	// Forgejo accepts a token as the basic-auth username for git-over-HTTP.
	u.User = url.User(c.token)
	u.Path = "/" + url.PathEscape(owner) + "/" + url.PathEscape(name) + ".git"
	return u.String(), nil
}
