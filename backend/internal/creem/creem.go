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
	"net/url"
	"strings"
	"time"
)

// DefaultBaseURL is Creem's production API.
const DefaultBaseURL = "https://api.creem.io"

// Config carries the API credentials for the Quill app.
type Config struct {
	APIKey           string
	WebhookSecret    string
	TeamProductID    string // Team Pro (per user)
	StarterProductID string // the individual "Starter" plan product
	ProProductID     string // the individual "Pro" plan product
	BaseURL          string // overridable for tests; empty = DefaultBaseURL
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

// StarterProductID returns the configured product id for Quill's individual
// Starter plan.
func (c *Client) StarterProductID() string {
	return c.cfg.StarterProductID
}

// ProProductID returns the configured product id for Quill's individual Pro
// plan (personal-account checkout sessions sell this).
func (c *Client) ProProductID() string {
	return c.cfg.ProProductID
}

// Checkout is the subset of a Creem checkout session Quill uses.
type Checkout struct {
	ID          string `json:"id"`
	CheckoutURL string `json:"checkout_url"`
}

// CheckoutInput describes a checkout session to open.
type CheckoutInput struct {
	ProductID  string
	SuccessURL string
	RequestID  string
	// Units is the seat count for per-seat products (Team is priced per seat);
	// values below 1 are sent as 1.
	Units int
	// CustomerEmail pre-fills the checkout's email field (optional).
	CustomerEmail string
	// Metadata survives to the webhook (checkout and subscription objects) so
	// the payment can be attributed to the right Quill tenant.
	Metadata map[string]string
}

// CreateCheckout opens a hosted checkout session (POST /v1/checkouts).
func (c *Client) CreateCheckout(ctx context.Context, in CheckoutInput) (*Checkout, error) {
	units := in.Units
	if units < 1 {
		units = 1
	}
	payload := map[string]any{
		"product_id":  in.ProductID,
		"success_url": in.SuccessURL,
		"request_id":  in.RequestID,
		"units":       units,
		"metadata":    in.Metadata,
	}
	if in.CustomerEmail != "" {
		payload["customer"] = map[string]string{"email": in.CustomerEmail}
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	// Creem's API lives under /v1 (the old /api/v1 prefix their docs once
	// showed was removed; /api/v1/checkouts now 404s before key validation).
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.BaseURL+"/v1/checkouts", bytes.NewReader(body))
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

// VerifySignature checks a webhook's creem-signature header: the lowercase hex
// HMAC-SHA256 of the raw request body keyed with the webhook secret (Creem's
// documented scheme; there is no timestamp component). Comparison is
// constant-time. Events are applied idempotently, so a replayed event cannot
// do more than re-assert the state it already described.
func VerifySignature(secret, body []byte, header string) bool {
	header = strings.TrimSpace(header)
	if len(secret) == 0 || header == "" {
		return false
	}
	got, err := hex.DecodeString(header)
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	return hmac.Equal(mac.Sum(nil), got)
}

// Sign computes the creem-signature value for body (used by tests and local
// tooling that replays events).
func Sign(secret, body []byte) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

// Event is the normalized subset of a Creem webhook event Quill acts on.
type Event struct {
	ID             string
	Type           string
	ProductID      string
	CustomerID     string
	SubscriptionID string
	Metadata       map[string]string
}

// ref is an expandable reference: Creem sends some relations either as an id
// string or as the expanded object.
type ref struct{ ID string }

func (r *ref) UnmarshalJSON(b []byte) error {
	if len(b) > 0 && b[0] == '"' {
		return json.Unmarshal(b, &r.ID)
	}
	var obj struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(b, &obj); err != nil {
		return nil // unknown shape: treat as absent rather than reject the event
	}
	r.ID = obj.ID
	return nil
}

// ParseEvent decodes a webhook body. Checkout events carry the checkout in
// "object" (product/customer/subscription expanded); subscription events
// carry the subscription itself (its id is the subscription id).
func ParseEvent(body []byte) (Event, error) {
	var raw struct {
		ID        string `json:"id"`
		EventType string `json:"eventType"`
		Object    struct {
			ID           string         `json:"id"`
			Object       string         `json:"object"`
			Product      ref            `json:"product"`
			Customer     ref            `json:"customer"`
			Subscription ref            `json:"subscription"`
			Metadata     map[string]any `json:"metadata"`
		} `json:"object"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return Event{}, err
	}
	evt := Event{
		ID:         raw.ID,
		Type:       raw.EventType,
		ProductID:  raw.Object.Product.ID,
		CustomerID: raw.Object.Customer.ID,
		Metadata:   map[string]string{},
	}
	if raw.Object.Object == "subscription" || strings.HasPrefix(raw.EventType, "subscription.") {
		evt.SubscriptionID = raw.Object.ID
	} else {
		evt.SubscriptionID = raw.Object.Subscription.ID
	}
	for k, v := range raw.Object.Metadata {
		if s, ok := v.(string); ok {
			evt.Metadata[k] = s
		}
	}
	return evt, nil
}

// SubscriptionItem is one line of a subscription (Team has a single item
// whose units are the billed seats).
type SubscriptionItem struct {
	ID        string `json:"id"`
	ProductID string `json:"product_id"`
	PriceID   string `json:"price_id,omitempty"`
	Units     int    `json:"units"`
}

// Subscription is the subset of a Creem subscription Quill uses.
type Subscription struct {
	ID     string             `json:"id"`
	Status string             `json:"status"`
	Items  []SubscriptionItem `json:"items"`
}

// Seat proration for mid-cycle changes: the difference is calculated now and
// charged (or credited) with the next invoice, so adding a teammate never
// triggers a surprise card charge on its own.
const SeatUpdateBehavior = "proration-charge"

// GetSubscription fetches a subscription (GET /v1/subscriptions?subscription_id=).
func (c *Client) GetSubscription(ctx context.Context, id string) (*Subscription, error) {
	var out Subscription
	if err := c.do(ctx, http.MethodGet, "/v1/subscriptions?subscription_id="+url.QueryEscape(id), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdateSubscriptionUnits sets a subscription item's units (seats) with
// SeatUpdateBehavior (POST /v1/subscriptions/{id}). The item is identified by
// its id plus its price (or product) id, which Creem validates.
func (c *Client) UpdateSubscriptionUnits(ctx context.Context, subscriptionID string, item SubscriptionItem, units int) error {
	line := map[string]any{"id": item.ID, "units": units}
	if item.PriceID != "" {
		line["price_id"] = item.PriceID
	} else {
		line["product_id"] = item.ProductID
	}
	body := map[string]any{"items": []any{line}, "update_behavior": SeatUpdateBehavior}
	return c.do(ctx, http.MethodPost, "/v1/subscriptions/"+url.PathEscape(subscriptionID), body, nil)
}

// do performs an authenticated JSON request against the Creem API.
func (c *Client) do(ctx context.Context, method, path string, in, out any) error {
	var rdr io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.cfg.BaseURL+path, rdr)
	if err != nil {
		return err
	}
	req.Header.Set("x-api-key", c.cfg.APIKey)
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	rb, _ := io.ReadAll(resp.Body)
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("creem: %s %s: %s: %s", method, strings.SplitN(path, "?", 2)[0], resp.Status, strings.TrimSpace(string(rb)))
	}
	if out != nil {
		if err := json.Unmarshal(rb, out); err != nil {
			return fmt.Errorf("creem: decode %s: %w", path, err)
		}
	}
	return nil
}
