// Package forgejo is a thin, typed client for the subset of Forgejo's REST API
// that Quill drives as an administrator. Quill never forks Forgejo: git storage
// and low-level repo/PR primitives live in Forgejo, and this client wraps them so
// Quill can provision users, organizations, and repositories that mirror its own
// Postgres metadata.
package forgejo

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

// Client talks to a Forgejo instance using an admin access token.
type Client struct {
	base  string
	token string
	http  *http.Client
}

// New builds a client from configuration. When the base URL or admin token is
// empty the client is considered disabled (see Enabled) and callers should skip
// provisioning so Quill still runs without Forgejo in local development.
func New(cfg config.ForgejoConfig) *Client {
	return &Client{
		base:  strings.TrimRight(cfg.BaseURL, "/"),
		token: cfg.AdminToken,
		http:  &http.Client{Timeout: 15 * time.Second},
	}
}

// Enabled reports whether the client is configured to call Forgejo.
func (c *Client) Enabled() bool { return c.base != "" && c.token != "" }

// APIError is returned for non-2xx responses from Forgejo.
type APIError struct {
	Status  int
	Method  string
	Path    string
	Message string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("forgejo %s %s: %d %s", e.Method, e.Path, e.Status, e.Message)
}

// NotFound reports whether err is an APIError with a 404 status.
func NotFound(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.Status == http.StatusNotFound
}

// ---- request plumbing ------------------------------------------------------

func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	var reader io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal body: %w", err)
		}
		reader = bytes.NewReader(buf)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.base+"/api/v1"+path, reader)
	if err != nil {
		return fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "token "+c.token)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("forgejo %s %s: %w", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &APIError{Status: resp.StatusCode, Method: method, Path: path, Message: readError(resp.Body)}
	}
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}
	return nil
}

// readError extracts Forgejo's { "message": ... } error envelope, falling back to
// the raw body.
func readError(r io.Reader) string {
	raw, _ := io.ReadAll(io.LimitReader(r, 8<<10))
	var env struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(raw, &env) == nil && env.Message != "" {
		return env.Message
	}
	return strings.TrimSpace(string(raw))
}
