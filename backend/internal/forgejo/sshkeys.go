package forgejo

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
)

// SSHKey represents a Forgejo user SSH public key.
type SSHKey struct {
	ID          int64  `json:"id"`
	Title       string `json:"title"`
	Key         string `json:"key"`
	Fingerprint string `json:"fingerprint"`
}

// ListSSHKeys returns the SSH public keys registered for username. The admin
// token has permission to read any user's keys.
func (c *Client) ListSSHKeys(ctx context.Context, username string) ([]SSHKey, error) {
	var out []SSHKey
	err := c.do(ctx, http.MethodGet, "/users/"+url.PathEscape(username)+"/keys", nil, &out)
	if err != nil {
		return nil, err
	}
	if out == nil {
		out = []SSHKey{}
	}
	return out, nil
}

// addSSHKeyBody is the admin-create payload for Forgejo's admin key endpoint.
type addSSHKeyBody struct {
	Title    string `json:"key_name"`
	Key      string `json:"key"`
	ReadOnly bool   `json:"read_only"`
}

// AddSSHKey adds a public SSH key for username via the admin API. The key must
// be an authorized-keys-format public key (e.g. "ssh-ed25519 AAAA... comment").
func (c *Client) AddSSHKey(ctx context.Context, username, title, publicKey string) (SSHKey, error) {
	body := addSSHKeyBody{Title: title, Key: publicKey, ReadOnly: false}
	var out SSHKey
	err := c.do(ctx, http.MethodPost, "/admin/users/"+url.PathEscape(username)+"/keys", body, &out)
	return out, err
}

// DeleteSSHKey removes a specific SSH key (by Forgejo key ID) from username via
// the admin API.
func (c *Client) DeleteSSHKey(ctx context.Context, username string, keyID int64) error {
	path := "/admin/users/" + url.PathEscape(username) + "/keys/" + strconv.FormatInt(keyID, 10)
	return c.do(ctx, http.MethodDelete, path, nil, nil)
}
