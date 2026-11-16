package creem

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Payloads below follow the shapes in Creem's webhook docs
// (docs.creem.io/code/webhooks): the event's data sits under "object";
// checkout objects expand product/customer/subscription; subscription events
// carry the subscription itself. The earlier implementation parsed "obj" and
// "product_id" and expected a "t=…,sha256=…" signature — none of which Creem
// sends — so no real webhook could ever have upgraded a plan.

const checkoutCompleted = `{
  "id": "evt_5WHHcZPv7VS0YUsberIuOz",
  "eventType": "checkout.completed",
  "created_at": 1728734325927,
  "object": {
    "id": "ch_4l0N34kxo16AhRKUHFUuXr",
    "object": "checkout",
    "request_id": "acme-1731700000",
    "order": {"id": "ord_4aDwWXjMLpes4Kj4XqNnUA", "customer": "cust_1OcIK1GEuVvXZwD19tjq2z", "product": "prod_team", "amount": 1000, "currency": "EUR", "status": "paid", "type": "recurring"},
    "product": {"id": "prod_team", "object": "product", "name": "Quill Team", "price": 500, "currency": "EUR", "billing_type": "recurring", "billing_period": "every-month"},
    "units": 2,
    "customer": {"id": "cust_1OcIK1GEuVvXZwD19tjq2z", "object": "customer", "email": "admin@acme.test", "name": "Ada Admin"},
    "subscription": {"id": "sub_6pC2lNB6joCRQIZ1aMrTpi", "object": "subscription", "product": "prod_team", "customer": "cust_1OcIK1GEuVvXZwD19tjq2z", "status": "active", "metadata": {"tenantSlug": "acme"}},
    "custom_fields": [],
    "status": "completed",
    "metadata": {"tenantSlug": "acme"},
    "mode": "test"
  }
}`

const subscriptionCanceled = `{
  "id": "evt_2iGTc600qGW6FBzloh2Nr7",
  "eventType": "subscription.canceled",
  "created_at": 1728734337932,
  "object": {
    "id": "sub_6pC2lNB6joCRQIZ1aMrTpi",
    "object": "subscription",
    "product": {"id": "prod_team", "object": "product", "name": "Quill Team"},
    "customer": {"id": "cust_1OcIK1GEuVvXZwD19tjq2z", "object": "customer", "email": "admin@acme.test"},
    "collection_method": "charge_automatically",
    "status": "canceled",
    "canceled_at": "2026-11-16T12:00:00.000Z",
    "metadata": {"tenantSlug": "acme"},
    "mode": "test"
  }
}`

func TestCreateCheckout(t *testing.T) {
	var gotKey, gotPath string
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("x-api-key")
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&got)
		fmt.Fprint(w, `{"id":"ch_123","checkout_url":"https://checkout.creem.io/ch_123"}`)
	}))
	defer srv.Close()

	c := New(Config{APIKey: "sk_test", TeamProductID: "prod_team", BaseURL: srv.URL})
	if !c.Enabled() {
		t.Fatal("client with API key should be enabled")
	}
	co, err := c.CreateCheckout(context.Background(), CheckoutInput{
		ProductID: "prod_team", SuccessURL: "https://app.example.test/success", RequestID: "req-1",
		Units: 3, CustomerEmail: "admin@acme.test", Metadata: map[string]string{"tenantSlug": "acme"},
	})
	if err != nil {
		t.Fatalf("CreateCheckout: %v", err)
	}
	if co.CheckoutURL != "https://checkout.creem.io/ch_123" || gotKey != "sk_test" || gotPath != "/v1/checkouts" {
		t.Fatalf("checkout=%+v key=%q path=%q", co, gotKey, gotPath)
	}
	if got["product_id"] != "prod_team" || got["units"] != float64(3) || got["request_id"] != "req-1" || got["success_url"] == "" {
		t.Fatalf("request body = %v", got)
	}
	if cust, _ := got["customer"].(map[string]any); cust["email"] != "admin@acme.test" {
		t.Fatalf("customer email not sent: %v", got["customer"])
	}
	if md, _ := got["metadata"].(map[string]any); md["tenantSlug"] != "acme" {
		t.Fatalf("metadata not sent: %v", got["metadata"])
	}

	// Units below 1 are clamped (a seat-priced product needs a quantity).
	if _, err := c.CreateCheckout(context.Background(), CheckoutInput{ProductID: "prod_team"}); err != nil {
		t.Fatal(err)
	}
	if got["units"] != float64(1) {
		t.Fatalf("units clamp: %v", got["units"])
	}

	if New(Config{}).Enabled() {
		t.Fatal("empty API key must disable the client")
	}
}

func TestCreateCheckoutSurfacesErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message":"product not found"}`, http.StatusNotFound)
	}))
	defer srv.Close()
	_, err := New(Config{APIKey: "k", BaseURL: srv.URL}).CreateCheckout(context.Background(), CheckoutInput{ProductID: "x"})
	if err == nil {
		t.Fatal("4xx from Creem must be an error")
	}
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"id":"ch_1"}`)
	}))
	defer srv2.Close()
	if _, err := New(Config{APIKey: "k", BaseURL: srv2.URL}).CreateCheckout(context.Background(), CheckoutInput{ProductID: "x"}); err == nil {
		t.Fatal("a response without checkout_url must be an error")
	}
}

// TestVerifySignatureCreemScheme pins Creem's documented scheme: hex
// HMAC-SHA256(secret, raw body), computed independently here.
func TestVerifySignatureCreemScheme(t *testing.T) {
	secret := []byte("whsec_test")
	body := []byte(checkoutCompleted)
	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	sig := hex.EncodeToString(mac.Sum(nil))

	if !VerifySignature(secret, body, sig) {
		t.Fatal("valid Creem signature rejected")
	}
	if Sign(secret, body) != sig {
		t.Fatal("Sign disagrees with the documented scheme")
	}
	if VerifySignature(secret, append([]byte{}, append(body, ' ')...), sig) {
		t.Fatal("tampered body accepted")
	}
	if VerifySignature([]byte("wrong"), body, sig) {
		t.Fatal("wrong secret accepted")
	}
	if VerifySignature(nil, body, Sign(nil, body)) {
		t.Fatal("empty secret must never verify")
	}
	for _, h := range []string{"", "garbage", "t=1,sha256=" + sig} {
		if VerifySignature(secret, body, h) {
			t.Fatalf("header %q accepted", h)
		}
	}
}

func TestParseEvent(t *testing.T) {
	evt, err := ParseEvent([]byte(checkoutCompleted))
	if err != nil {
		t.Fatal(err)
	}
	want := Event{ID: "evt_5WHHcZPv7VS0YUsberIuOz", Type: "checkout.completed", ProductID: "prod_team",
		CustomerID: "cust_1OcIK1GEuVvXZwD19tjq2z", SubscriptionID: "sub_6pC2lNB6joCRQIZ1aMrTpi"}
	if evt.ID != want.ID || evt.Type != want.Type || evt.ProductID != want.ProductID ||
		evt.CustomerID != want.CustomerID || evt.SubscriptionID != want.SubscriptionID || evt.Metadata["tenantSlug"] != "acme" {
		t.Fatalf("checkout.completed parsed as %+v", evt)
	}

	evt, err = ParseEvent([]byte(subscriptionCanceled))
	if err != nil {
		t.Fatal(err)
	}
	if evt.Type != "subscription.canceled" || evt.SubscriptionID != "sub_6pC2lNB6joCRQIZ1aMrTpi" ||
		evt.ProductID != "prod_team" || evt.CustomerID != "cust_1OcIK1GEuVvXZwD19tjq2z" || evt.Metadata["tenantSlug"] != "acme" {
		t.Fatalf("subscription.canceled parsed as %+v", evt)
	}

	// Relations sent as bare ids parse too; non-string metadata is skipped.
	evt, err = ParseEvent([]byte(`{"eventType":"subscription.paid","object":{"id":"sub_1","object":"subscription","product":"prod_x","customer":"cust_x","metadata":{"tenantSlug":"a","n":3}}}`))
	if err != nil || evt.ProductID != "prod_x" || evt.CustomerID != "cust_x" || evt.SubscriptionID != "sub_1" || evt.Metadata["tenantSlug"] != "a" || len(evt.Metadata) != 1 {
		t.Fatalf("id-form event = %+v err=%v", evt, err)
	}

	if _, err := ParseEvent([]byte(`not json`)); err == nil {
		t.Fatal("malformed body must error")
	}
}
