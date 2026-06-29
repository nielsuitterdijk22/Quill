// Package zitadel is a thin, typed client for the subset of Zitadel's Management
// and v2 APIs that Quill drives with a service-account token: creating an
// organisation for a new workspace and managing its members. Authentication
// (verifying user tokens) lives in internal/auth; this package is the server-side
// administrative counterpart used during onboarding and org settings.
package zitadel

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/nielsuitterdijk22/quill/internal/config"
)

// Client talks to a Zitadel instance using a service-account token (PAT or
// machine-key JWT) with org-management permissions.
type Client struct {
	base  string
	token string
	http  *http.Client
}

// New builds a client from configuration. When the issuer or management token is
// empty the client is disabled (see Enabled) and callers should skip org-side
// provisioning so Quill still runs without Zitadel admin access.
func New(cfg config.ZitadelConfig) *Client {
	return &Client{
		base:  strings.TrimRight(cfg.Issuer, "/"),
		token: cfg.ManagementToken,
		http:  &http.Client{Timeout: 15 * time.Second},
	}
}

// Enabled reports whether the client is configured to call Zitadel.
func (c *Client) Enabled() bool { return c.base != "" && c.token != "" }

// APIError is returned for non-2xx responses from Zitadel.
type APIError struct {
	Status  int
	Method  string
	Path    string
	Message string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("zitadel %s %s: %d %s", e.Method, e.Path, e.Status, e.Message)
}

// NotFound reports whether err is an APIError with a 404 status.
func NotFound(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.Status == http.StatusNotFound
}

// do issues a JSON request to path (relative to the issuer), decoding a 2xx
// response into out (when non-nil). orgID, when set, scopes the call to that
// organisation via the x-zitadel-orgid header.
func (c *Client) do(ctx context.Context, method, path, orgID string, body, out any) error {
	var reader io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(buf)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	if orgID != "" {
		req.Header.Set("x-zitadel-orgid", orgID)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode/100 != 2 {
		return &APIError{Status: resp.StatusCode, Method: method, Path: path, Message: strings.TrimSpace(string(raw))}
	}
	if out != nil && len(raw) > 0 {
		return json.Unmarshal(raw, out)
	}
	return nil
}
