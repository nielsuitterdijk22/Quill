// Package zitadel provides a thin client over the Zitadel Management API for the
// one operation Quill drives from the platform layer: inviting members by email
// through Zitadel's own mail service (the same service that sends signup
// verification). Quill does not provision Zitadel orgs or identity providers —
// SSO customers' orgs are set up by hand in the Zitadel console (see
// platform/sso.go).
//
// It is deliberately best-effort and optional. When Zitadel is not configured
// (local / self-hosted-without-Zitadel), the platform falls back to Quill-only
// orgs and shareable invite links, so nothing here is on the critical path. The
// request shapes target the Zitadel v1 Management API; failures are surfaced to
// the caller, which logs and continues.
package zitadel

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client calls the Zitadel Management API with a service-account token
// (ZITADEL_MANAGEMENT_TOKEN). A zero-value or unconfigured client reports
// Enabled() == false and performs no network calls.
type Client struct {
	issuer    string
	mgmtToken string
	http      *http.Client
}

// NewClient builds a management client. issuer is the Zitadel instance base URL
// (e.g. https://auth.example.com); mgmtToken is a service-account PAT with
// permission to import users. issuer or mgmtToken being empty disables the client.
func NewClient(issuer, mgmtToken string) *Client {
	return &Client{
		issuer:    strings.TrimRight(strings.TrimSpace(issuer), "/"),
		mgmtToken: strings.TrimSpace(mgmtToken),
		http:      &http.Client{Timeout: 15 * time.Second},
	}
}

// Enabled reports whether the client is configured to make calls.
func (c *Client) Enabled() bool {
	return c != nil && c.issuer != "" && c.mgmtToken != ""
}

// InviteUser creates a human user in orgID with the given email so Zitadel sends
// its initialization/invite email: with no password and an unverified email, the
// created user is in the "initial" state and Zitadel mails an init code for the
// invitee to set up their account (the same flow as signup verification).
//
// It uses the create endpoint (/users/human), NOT /users/human/_import — the
// _import endpoint is for silent bulk migration and does NOT send any email, which
// is why invites appeared to send no mail. Requires a working SMTP configuration
// in Zitadel; that is Zitadel's concern.
func (c *Client) InviteUser(ctx context.Context, orgID, email, displayName string) error {
	if !c.Enabled() {
		return fmt.Errorf("zitadel management client not configured")
	}
	first, last := splitName(displayName, email)
	body := map[string]any{
		"userName": email,
		"profile": map[string]any{
			"firstName":   first,
			"lastName":    last,
			"displayName": strings.TrimSpace(displayName),
		},
		"email": map[string]any{
			"email":           email,
			"isEmailVerified": false,
		},
	}
	return c.do(ctx, http.MethodPost, "/management/v1/users/human", orgID, body, nil)
}

// do performs a JSON request against the Management API and errors on any
// non-2xx response. orgID, when non-empty, scopes the call to that organization
// via the x-zitadel-orgid header. out, when non-nil, receives the decoded
// response body.
func (c *Client) do(ctx context.Context, method, path, orgID string, body, out any) error {
	status, snippet, err := c.doStatus(ctx, method, path, orgID, body, out)
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		return &apiError{method: method, path: path, status: status, snippet: snippet}
	}
	return nil
}

// doTolerant is do for idempotent calls: a 404 (already gone) or 409 (already
// exists) is treated as success, so re-running provisioning is safe.
func (c *Client) doTolerant(ctx context.Context, method, path, orgID string, body any) error {
	status, snippet, err := c.doStatus(ctx, method, path, orgID, body, nil)
	if err != nil {
		return err
	}
	if status == http.StatusNotFound || status == http.StatusConflict {
		return nil
	}
	if status < 200 || status >= 300 {
		return &apiError{method: method, path: path, status: status, snippet: snippet}
	}
	return nil
}

// doStatus performs the request and returns the HTTP status and (on a non-2xx
// response) a truncated body snippet. The returned error is reserved for
// transport / encode / decode failures — an HTTP error status is NOT an error
// here, so callers can branch on the status (e.g. treat 409 as "already exists").
// out is decoded only on a 2xx response.
func (c *Client) doStatus(ctx context.Context, method, path, orgID string, body, out any) (int, string, error) {
	var reader io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return 0, "", fmt.Errorf("encode request: %w", err)
		}
		reader = bytes.NewReader(buf)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.issuer+path, reader)
	if err != nil {
		return 0, "", err
	}
	req.Header.Set("Authorization", "Bearer "+c.mgmtToken)
	req.Header.Set("Content-Type", "application/json")
	if orgID != "" {
		req.Header.Set("x-zitadel-orgid", orgID)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return resp.StatusCode, strings.TrimSpace(string(snippet)), nil
	}
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return resp.StatusCode, "", fmt.Errorf("decode response: %w", err)
		}
	}
	return resp.StatusCode, "", nil
}

// apiError is a non-2xx Management API response. It carries the status so callers
// can treat "already exists" (409) as non-fatal without string matching.
type apiError struct {
	method  string
	path    string
	status  int
	snippet string
}

func (e *apiError) Error() string {
	return fmt.Sprintf("zitadel %s %s returned %d: %s", e.method, e.path, e.status, e.snippet)
}

// splitName derives a first/last name for a Zitadel human profile, which requires
// both. It splits a display name on the first space; with no usable display name
// it falls back to the email local part, and never returns an empty last name
// (Zitadel rejects one).
func splitName(displayName, email string) (first, last string) {
	displayName = strings.TrimSpace(displayName)
	if displayName == "" {
		if at := strings.IndexByte(email, '@'); at > 0 {
			displayName = email[:at]
		} else {
			displayName = email
		}
	}
	parts := strings.SplitN(displayName, " ", 2)
	first = parts[0]
	if len(parts) == 2 && strings.TrimSpace(parts[1]) != "" {
		last = strings.TrimSpace(parts[1])
	} else {
		last = "(invited)"
	}
	return first, last
}
