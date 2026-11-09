// Package creem talks to the Creem merchant-of-record API: creating checkout
// sessions for plan upgrades and verifying webhook signatures. Creem handles
// card capture, subscriptions, invoicing, and global sales tax; Quill only
// starts checkouts and reacts to webhook events.
package creem

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// DefaultBaseURL is Creem's production API.
const DefaultBaseURL = "https://api.creem.io"

// Config carries the API credentials for the Quill app.
type Config struct {
	APIKey        string
	WebhookSecret string
	TeamProductID string
	BaseURL       string // overridable for tests; empty = DefaultBaseURL
}

// Client wraps the Creem HTTP API.
type Client struct {
	cfg  Config
	http *http.Client
}

// New builds a client. An empty API key leaves the client disabled.
func New(cfg Config) *Client {
	if cfg.BaseURL == "" {
		cfg.BaseURL = DefaultBaseURL
	}
	return &Client{cfg: cfg, http: &http.Client{Timeout: 15 * time.Second}}
}

// Enabled reports whether checkout creation can be attempted.
func (c *Client) Enabled() bool {
	return c.cfg.APIKey != ""
}

// WebhookConfigured reports whether webhook events can be verified.
func (c *Client) WebhookConfigured() bool {
	return c.cfg.WebhookSecret != ""
}

// TeamProductID returns the configured product id for Quill's team plan (the
// plan checkout sessions sell).
func (c *Client) TeamProductID() string {
	return c.cfg.TeamProductID
}

// WebhookSecret exposes the configured webhook signing secret for signature
// verification by the platform layer.
func (c *Client) WebhookSecret() string {
	return c.cfg.WebhookSecret
}

// Checkout is the subset of a Creem checkout session Quill uses.
type Checkout struct {
	ID          string `json:"id"`
	CheckoutURL string `json:"checkout_url"`
	CustomerID  string `json:"customer_id,omitempty"`
	CustomerURL string `json:"customer_url,omitempty"`
}

// CreateCheckout opens a checkout session for a product (e.g. the team-plan
// product) with metadata that survives to the webhook so the completed checkout
// can be attributed to the right Quill org.
func (c *Client) CreateCheckout(ctx context.Context, productID, successURL, requestID string, metadata map[string]string) (*Checkout, error) {
	body, err := json.Marshal(map[string]any{
		"product_id":  productID,
		"success_url": successURL,
		"request_id":  requestID,
		"metadata":    metadata,
	})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.BaseURL+"/api/v1/checkouts", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("x-api-key", c.cfg.APIKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	rb, _ := io.ReadAll(resp.Body)
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("creem: checkout failed: %s: %s", resp.Status, strings.TrimSpace(string(rb)))
	}
	var out Checkout
	if err := json.Unmarshal(rb, &out); err != nil {
		return nil, err
	}
	if out.CheckoutURL == "" {
		return nil, fmt.Errorf("creem: checkout response missing checkout_url")
	}
	return &out, nil
}

// VerifySignature checks a Creem webhook's creem-signature header:
// "t=<unix seconds>,sha256=<hex hmac of '<t>.<body>'>". The timestamp bounds
// replay attacks (skewWindow).
func VerifySignature(secret, body []byte, header string, skewWindow time.Duration) bool {
	var ts, sigHex string
	for _, part := range strings.Split(header, ",") {
		k, v, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok {
			continue
		}
		switch k {
		case "t":
			ts = v
		case "sha256", "v1":
			sigHex = v
		}
	}
	if ts == "" || sigHex == "" {
		return false
	}
	sec, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return false
	}
	age := time.Since(time.Unix(sec, 0))
	if age < -skewWindow || age > skewWindow {
		return false
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(ts))
	mac.Write([]byte("."))
	mac.Write(body)
	return hmac.Equal(mac.Sum(nil), decodeHex(sigHex))
}

func decodeHex(s string) []byte {
	b, err := hex.DecodeString(s)
	if err != nil {
		return nil
	}
	return b
}
