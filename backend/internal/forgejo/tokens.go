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

// DeleteAccessToken revokes a personal access token by its name, authenticated as
// the user (Forgejo forbids deleting tokens via the admin token, mirroring
// creation). A 404 means the token is already gone, which callers may ignore.
func (c *Client) DeleteAccessToken(ctx context.Context, username, password, tokenName string) error {
	path := "/users/" + url.PathEscape(username) + "/tokens/" + url.PathEscape(tokenName)
	return c.doBasicAuth(ctx, http.MethodDelete, path, username, password, nil, nil)
}

// CloneAuth describes how the CI dispatcher should clone a Forgejo repository.
type CloneAuth struct {
	URL        string
	AuthHeader string
}

// CloneAuth returns an HTTP clone URL plus an auth header for owner/name. Forgejo
// accepts API tokens for git-over-HTTP via Authorization, while embedding the
// token in basic-auth credentials can be rejected for private repositories.
func (c *Client) CloneAuth(owner, name string) (CloneAuth, error) {
	if c.base == "" || c.token == "" {
		return CloneAuth{}, errors.New("forgejo client disabled")
	}
	u, err := url.Parse(c.base)
	if err != nil {
		return CloneAuth{}, err
	}
	u.Path = "/" + url.PathEscape(owner) + "/" + url.PathEscape(name) + ".git"
	return CloneAuth{URL: u.String(), AuthHeader: "Authorization: token " + c.token}, nil
}
