// Package zitadel provides a thin client over the Zitadel Management API for the
// operations Quill drives from the platform layer: provisioning an organization
// when a Quill org is created, and inviting members by email through Zitadel's
// own mail service (the same service that sends signup verification).
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
	"net/url"
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
// org-management permission. Either being empty disables the client.
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

// CreateOrg creates a Zitadel organization and returns its id. Used when a Quill
// org is created so members can be invited into it and, later, sign in against
// it (org claim -> Quill tenant).
func (c *Client) CreateOrg(ctx context.Context, name string) (string, error) {
	if !c.Enabled() {
		return "", fmt.Errorf("zitadel management client not configured")
	}
	var out struct {
		ID string `json:"id"`
	}
	if err := c.do(ctx, http.MethodPost, "/management/v1/orgs", "", map[string]any{
		"name": name,
	}, &out); err != nil {
		return "", err
	}
	if out.ID == "" {
		return "", fmt.Errorf("zitadel create org returned no id")
	}
	return out.ID, nil
}

// InviteUser creates a human user in orgID with the given email and lets Zitadel
// send its initialization/invite email (the account has no password, so Zitadel
// prompts the invitee to set one — the same flow as signup verification).
// Requires a working SMTP configuration in Zitadel; that is Zitadel's concern.
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
	return c.do(ctx, http.MethodPost, "/management/v1/users/human/_import", orgID, body, nil)
}

// CreateAccount registers a person in the instance's default organization with
// a password and sends Zitadel's email-verification code. Quill's sign-up form
// collects only a handle, an email and a password; Zitadel insists on a first
// and last name, so both are set to the handle (nobody ever sees them). The
// handle is the Zitadel username, which Quill derives its own username from at
// first sign-in. ErrConflict means the username or email is already taken.
//
// verifyURL, when non-empty, is the Zitadel urlTemplate for the emailed link
// ({{.UserID}} and {{.Code}} are filled in by Zitadel). Pointing it at a Quill
// page keeps the person in Quill: Zitadel's default link opens its own login
// without a Quill sign-in attached, which ends on its management console.
func (c *Client) CreateAccount(ctx context.Context, username, email, password, verifyURL string) error {
	if !c.Enabled() {
		return fmt.Errorf("zitadel management client not configured")
	}
	send := map[string]any{}
	if verifyURL != "" {
		send["urlTemplate"] = verifyURL
	}
	return c.do(ctx, http.MethodPost, "/v2/users/human", "", map[string]any{
		"username": username,
		"profile":  map[string]any{"givenName": username, "familyName": username, "displayName": username},
		"email":    map[string]any{"email": email, "sendCode": send},
		"password": map[string]any{"password": password, "changeRequired": false},
	}, nil)
}

// VerifyEmail confirms a human user's email with the code from the emailed link.
func (c *Client) VerifyEmail(ctx context.Context, userID, code string) error {
	if !c.Enabled() {
		return fmt.Errorf("zitadel management client not configured")
	}
	return c.do(ctx, http.MethodPost, "/v2/users/"+url.PathEscape(userID)+"/email/verify", "", map[string]any{"verificationCode": code}, nil)
}

// do performs a JSON request against the Management API. orgID, when non-empty,
// scopes the call to that organization via the x-zitadel-orgid header. out, when
// non-nil, receives the decoded response body.
func (c *Client) do(ctx context.Context, method, path, orgID string, body, out any) error {
	var reader io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encode request: %w", err)
		}
		reader = bytes.NewReader(buf)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.issuer+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.mgmtToken)
	req.Header.Set("Content-Type", "application/json")
	if orgID != "" {
		req.Header.Set("x-zitadel-orgid", orgID)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return classify(resp.StatusCode, fmt.Errorf("zitadel %s %s returned %d: %s", method, path, resp.StatusCode, strings.TrimSpace(string(snippet))))
	}
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}
	return nil
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
