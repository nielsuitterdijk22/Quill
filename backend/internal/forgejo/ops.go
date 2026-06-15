package forgejo

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/url"
	"strings"
)

// ---- types -----------------------------------------------------------------

// User is a Forgejo user account.
type User struct {
	ID       int64  `json:"id"`
	Login    string `json:"login"`
	Email    string `json:"email"`
	FullName string `json:"full_name"`
}

// Org is a Forgejo organization. The REST API exposes the handle as both "name"
// and the legacy "username"; we read whichever is present.
type Org struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Username string `json:"username"`
	FullName string `json:"full_name"`
}

// Handle returns the organization's canonical handle.
func (o Org) Handle() string {
	if o.Name != "" {
		return o.Name
	}
	return o.Username
}

// Repo is a Forgejo repository.
type Repo struct {
	ID            int64  `json:"id"`
	Name          string `json:"name"`
	FullName      string `json:"full_name"`
	Private       bool   `json:"private"`
	Empty         bool   `json:"empty"`
	DefaultBranch string `json:"default_branch"`
	HTMLURL       string `json:"html_url"`
	CloneURL      string `json:"clone_url"`
}

// ---- options ---------------------------------------------------------------

// CreateUserOptions describes a new Forgejo user. Quill mediates all access via
// the admin token, so the password is set to an unguessable value the user never
// needs; MustChangePassword is false because Quill, not Forgejo, owns login.
type CreateUserOptions struct {
	Username           string `json:"username"`
	Email              string `json:"email"`
	Password           string `json:"password"`
	MustChangePassword bool   `json:"must_change_password"`
}

// CreateOrgOptions describes a new organization.
type CreateOrgOptions struct {
	Name        string `json:"username"` // Forgejo expects the handle as "username"
	FullName    string `json:"full_name,omitempty"`
	Description string `json:"description,omitempty"`
	Visibility  string `json:"visibility,omitempty"` // public | limited | private
}

// CreateRepoOptions describes a new repository created under an organization.
type CreateRepoOptions struct {
	Name          string `json:"name"`
	Description   string `json:"description,omitempty"`
	Private       bool   `json:"private"`
	AutoInit      bool   `json:"auto_init"`
	DefaultBranch string `json:"default_branch,omitempty"`
}

// ---- operations ------------------------------------------------------------

// Version returns the Forgejo server version (also a lightweight connectivity check).
func (c *Client) Version(ctx context.Context) (string, error) {
	var out struct {
		Version string `json:"version"`
	}
	if err := c.do(ctx, http.MethodGet, "/version", nil, &out); err != nil {
		return "", err
	}
	return out.Version, nil
}

// CreateUser provisions a Forgejo user (admin).
func (c *Client) CreateUser(ctx context.Context, opts CreateUserOptions) (User, error) {
	var out User
	err := c.do(ctx, http.MethodPost, "/admin/users", opts, &out)
	return out, err
}

// GetUser fetches a Forgejo user by login.
func (c *Client) GetUser(ctx context.Context, username string) (User, error) {
	var out User
	err := c.do(ctx, http.MethodGet, "/users/"+url.PathEscape(username), nil, &out)
	return out, err
}

// DeleteUser removes a Forgejo user (admin). purge also deletes their content.
func (c *Client) DeleteUser(ctx context.Context, username string, purge bool) error {
	path := "/admin/users/" + url.PathEscape(username)
	if purge {
		path += "?purge=true"
	}
	return c.do(ctx, http.MethodDelete, path, nil, nil)
}

// CreateOrg provisions a Forgejo organization (owned by the admin token's user).
func (c *Client) CreateOrg(ctx context.Context, opts CreateOrgOptions) (Org, error) {
	var out Org
	err := c.do(ctx, http.MethodPost, "/orgs", opts, &out)
	return out, err
}

// GetOrg fetches a Forgejo organization by handle.
func (c *Client) GetOrg(ctx context.Context, name string) (Org, error) {
	var out Org
	err := c.do(ctx, http.MethodGet, "/orgs/"+url.PathEscape(name), nil, &out)
	return out, err
}

// DeleteOrg removes a Forgejo organization.
func (c *Client) DeleteOrg(ctx context.Context, name string) error {
	return c.do(ctx, http.MethodDelete, "/orgs/"+url.PathEscape(name), nil, nil)
}

// CreateOrgRepo creates a repository under an organization.
func (c *Client) CreateOrgRepo(ctx context.Context, org string, opts CreateRepoOptions) (Repo, error) {
	var out Repo
	err := c.do(ctx, http.MethodPost, "/orgs/"+url.PathEscape(org)+"/repos", opts, &out)
	return out, err
}

// GetRepo fetches a repository by owner and name.
func (c *Client) GetRepo(ctx context.Context, owner, name string) (Repo, error) {
	var out Repo
	err := c.do(ctx, http.MethodGet, "/repos/"+url.PathEscape(owner)+"/"+url.PathEscape(name), nil, &out)
	return out, err
}

// EditRepoOptions describes an edit to a repository. Every field is a pointer so
// only the ones the caller sets are sent (and changed); nil fields are omitted
// from the request body and left untouched in Forgejo. A non-nil pointer to the
// empty string is still sent, which is what lets a description be cleared.
type EditRepoOptions struct {
	Name          *string `json:"name,omitempty"`
	Description   *string `json:"description,omitempty"`
	Private       *bool   `json:"private,omitempty"`
	DefaultBranch *string `json:"default_branch,omitempty"`
	Archived      *bool   `json:"archived,omitempty"`
}

// EditRepo updates a repository's settings (PATCH /repos/{owner}/{repo}) and
// returns the repository's new state. Renaming sets Name; the owner is unchanged.
func (c *Client) EditRepo(ctx context.Context, owner, name string, opts EditRepoOptions) (Repo, error) {
	var out Repo
	p := "/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(name)
	err := c.do(ctx, http.MethodPatch, p, opts, &out)
	return out, err
}

// DeleteRepo removes a repository by owner and name.
func (c *Client) DeleteRepo(ctx context.Context, owner, name string) error {
	return c.do(ctx, http.MethodDelete, "/repos/"+url.PathEscape(owner)+"/"+url.PathEscape(name), nil, nil)
}

// CreateFile commits a new file at path on a branch (empty branch = default
// branch). content is raw bytes; the API expects base64 so it is encoded here.
func (c *Client) CreateFile(ctx context.Context, owner, repo, path string, content []byte, message, branch string) error {
	body := map[string]string{
		"message": message,
		"content": base64.StdEncoding.EncodeToString(content),
	}
	if branch != "" {
		body["branch"] = branch
	}
	p := "/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(repo) + "/contents/" + escapeContentsPath(strings.Trim(path, "/"))
	return c.do(ctx, http.MethodPost, p, body, nil)
}
