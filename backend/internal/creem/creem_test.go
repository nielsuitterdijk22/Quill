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
	"time"
)

func TestCreateCheckout(t *testing.T) {
	var gotKey, gotProduct, gotSuccess string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("x-api-key")
		var body struct {
			ProductID  string `json:"product_id"`
			SuccessURL string `json:"success_url"`
		}
		_ = unmarshalBody(r, &body)
		gotProduct = body.ProductID
		gotSuccess = body.SuccessURL
		fmt.Fprintf(w, `{"id":"ch_123","checkout_url":"https://pay.creem.io/session/abc"}`)
	}))
	defer srv.Close()

	c := New(Config{APIKey: "sk_test", TeamProductID: "prod_team", BaseURL: srv.URL})
	if !c.Enabled() {
		t.Fatal("client with API key should be enabled")
	}
	co, err := c.CreateCheckout(context.Background(), "prod_team", "https://app.example.test/success", "req-1", map[string]string{"orgSlug": "acme"})
	if err != nil {
		t.Fatalf("CreateCheckout: %v", err)
	}
	if co.CheckoutURL == "" || gotKey != "sk_test" || gotProduct != "prod_team" || gotSuccess == "" {
		t.Fatalf("checkout=%+v key=%q product=%q success=%q", co, gotKey, gotProduct, gotSuccess)
	}

	// Unconfigured client reports disabled.
	if New(Config{}).Enabled() {
		t.Fatal("empty API key must disable the client")
	}
}

func TestVerifySignature(t *testing.T) {
	secret := []byte("whsec_test")
	body := []byte(`{"eventType":"checkout.completed"}`)
	ts := time.Now().Unix()
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(fmt.Sprintf("%d.", ts)))
	mac.Write(body)
	sig := fmt.Sprintf("t=%d,sha256=%s", ts, hex.EncodeToString(mac.Sum(nil)))

	if !VerifySignature(secret, body, sig, 10*time.Minute) {
		t.Fatal("valid signature rejected")
	}
	if VerifySignature(secret, append(body, 'x'), sig, 10*time.Minute) {
		t.Fatal("tampered body accepted")
	}
	if VerifySignature([]byte("wrong"), body, sig, 10*time.Minute) {
		t.Fatal("wrong secret accepted")
	}
	old := fmt.Sprintf("t=%d,sha256=%s", ts-(60*60), hex.EncodeToString(mac.Sum(nil)))
	if VerifySignature(secret, body, old, 10*time.Minute) {
		t.Fatal("stale timestamp accepted (replay not bounded)")
	}
	if VerifySignature(secret, body, "garbage", 10*time.Minute) {
		t.Fatal("garbage header accepted")
	}
}

func unmarshalBody(r *http.Request, v any) error {
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(v)
}
