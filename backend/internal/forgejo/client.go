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
	"strconv"
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

// StatusCode returns the HTTP status of an APIError, or 0 if err is not one.
func StatusCode(err error) int {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.Status
	}
	return 0
}

// IsNetworkError reports whether err is a connectivity-level failure rather than
// an HTTP-level response from Forgejo (connection refused, DNS failure, timeout).
// Distinguishes "Forgejo is down" from "Forgejo responded with 4xx/5xx". Context
// cancellations (user navigated away) are excluded and not treated as downtime.
func IsNetworkError(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) {
		return false
	}
	return StatusCode(err) == 0
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

// doBasicAuth performs a request authenticated with a specific user's HTTP basic
// credentials rather than the admin token. Forgejo requires basic auth — not
// token auth — to mint a personal access token, so this exists for that narrow
// purpose and should not be used for ordinary admin-mediated calls.
func (c *Client) doBasicAuth(ctx context.Context, method, path, user, pass string, body, out any) error {
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
	req.SetBasicAuth(user, pass)

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

// count issues a GET and returns Forgejo's X-Total-Count header — the total
// number of items matching the query — without downloading the items. It returns
// 0 when the header is absent or unparseable.
func (c *Client) count(ctx context.Context, path string) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/api/v1"+path, nil)
	if err != nil {
		return 0, fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "token "+c.token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, fmt.Errorf("forgejo GET %s: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return 0, &APIError{Status: resp.StatusCode, Method: http.MethodGet, Path: path, Message: readError(resp.Body)}
	}
	n, _ := strconv.Atoi(resp.Header.Get("X-Total-Count"))
	return n, nil
}

// getRaw performs a GET and returns the raw response body. It is used for
// endpoints that return text rather than JSON (e.g. a pull request's unified
// diff). The body is capped to guard against unexpectedly large payloads.
func (c *Client) getRaw(ctx context.Context, path string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/api/v1"+path, nil)
	if err != nil {
		return nil, fmt.Errorf("new request: %w", err)
	}
	if c.token != "" {
		req.Header.Set("Authorization", "token "+c.token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("forgejo GET %s: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &APIError{Status: resp.StatusCode, Method: http.MethodGet, Path: path, Message: readError(resp.Body)}
	}
	return io.ReadAll(io.LimitReader(resp.Body, limit))
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
