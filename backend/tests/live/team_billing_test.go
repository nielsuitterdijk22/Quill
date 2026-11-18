package live

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

// TestTeamOnboardingAndBilling walks the paid-teams onboarding against the
// deployed platform: a new organization starts inactive (locked: there
// is no free tier or trial), its admin can open a Creem Team checkout, and — when the
// operator supplies the webhook secret and product id — a Creem-signed
// checkout.completed moves it to team while a signed subscription.canceled
// locks it (repo creation answers 402, existing repos stay readable).
//
// Optional env for the webhook half (values from Secret Manager /quill/deploy):
// QUILL_E2E_CREEM_WEBHOOK_SECRET, QUILL_E2E_CREEM_TEAM_PRODUCT.
func TestTeamOnboardingAndBilling(t *testing.T) {
	requireLive(t)
	token := login(t)
	org := fmt.Sprintf("e2e-team-%d", time.Now().UnixNano()%1e9)

	if st, b := apiCall(t, token, http.MethodPost, "/orgs", map[string]any{"slug": org, "name": "E2E Team"}); st != 200 && st != 201 {
		t.Fatalf("create org: %d %s", st, b)
	}

	st, b := apiCall(t, token, http.MethodGet, "/orgs/"+org+"/billing", nil)
	if st != 200 {
		t.Fatalf("billing: %d %s", st, b)
	}
	bill := jbody(t, b)
	if bill["plan"] != "inactive" || bill["locked"] != true || bill["seats"] != float64(1) {
		t.Fatalf("new org billing = %s, want an inactive, locked org with 1 seat", b)
	}
	repo := org + "-r1"
	if st, b := apiCall(t, token, http.MethodPost, "/projects/"+org+"/repos", map[string]any{"slug": repo, "name": repo}); st == 200 || st == 201 {
		t.Fatalf("create repo on an inactive org must be refused, got %d %s", st, b)
	}

	st, b = apiCall(t, token, http.MethodPost, "/orgs/"+org+"/billing/checkout", map[string]any{})
	switch st {
	case 200:
		u := fmt.Sprint(jbody(t, b)["url"])
		if !strings.HasPrefix(u, "https://") || !strings.Contains(u, "creem.io") {
			t.Fatalf("checkout url = %q", u)
		}
		t.Logf("checkout opened: %s", u)
	case 501:
		t.Logf("payments not configured on this deployment: %s", b)
	default:
		t.Fatalf("checkout: %d %s", st, b)
	}

	secret, product := os.Getenv("QUILL_E2E_CREEM_WEBHOOK_SECRET"), os.Getenv("QUILL_E2E_CREEM_TEAM_PRODUCT")
	if secret == "" || product == "" {
		t.Log("QUILL_E2E_CREEM_WEBHOOK_SECRET/QUILL_E2E_CREEM_TEAM_PRODUCT unset: skipping the webhook half")
		return
	}
	sub := "sub_e2e_" + org
	hook := func(body string) int {
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write([]byte(body))
		// Sent raw: the signature covers these exact bytes.
		r, err := http.NewRequest(http.MethodPost, apiURL+"/api/v1/webhooks/creem", bytes.NewReader([]byte(body)))
		if err != nil {
			t.Fatal(err)
		}
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("creem-signature", hex.EncodeToString(mac.Sum(nil)))
		resp, err := client(t).Do(r)
		if err != nil {
			t.Fatalf("webhook: %v", err)
		}
		rb, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatalf("webhook: %d %s", resp.StatusCode, rb)
		}
		return resp.StatusCode
	}
	plan := func() map[string]any {
		st, b := apiCall(t, token, http.MethodGet, "/orgs/"+org+"/billing", nil)
		if st != 200 {
			t.Fatalf("billing: %d %s", st, b)
		}
		return jbody(t, b)
	}

	hook(fmt.Sprintf(`{"id":"evt_e2e_%[1]s","eventType":"checkout.completed","object":{"id":"ch_e2e_%[1]s","object":"checkout","product":{"id":%[2]q},"customer":{"id":"cust_e2e"},"subscription":{"id":%[3]q},"status":"completed","metadata":{"tenantSlug":%[1]q},"mode":"test"}}`, org, product, sub))
	if p := plan(); p["plan"] != "team" || p["locked"] != false {
		t.Fatalf("after checkout.completed: %v", p)
	}
	if st, b := apiCall(t, token, http.MethodPost, "/projects/"+org+"/repos", map[string]any{"slug": repo, "name": repo}); st != 200 && st != 201 {
		t.Fatalf("create repo on a paid org: %d %s", st, b)
	}

	hook(fmt.Sprintf(`{"id":"evt_e2e_cancel_%[1]s","eventType":"subscription.canceled","object":{"id":%[3]q,"object":"subscription","product":{"id":%[2]q},"customer":{"id":"cust_e2e"},"status":"canceled","metadata":{"tenantSlug":%[1]q},"mode":"test"}}`, org, product, sub))
	if p := plan(); p["plan"] != "lapsed" || p["locked"] != true {
		t.Fatalf("after subscription.canceled: %v", p)
	}
	if st, b := apiCall(t, token, http.MethodPost, "/projects/"+org+"/repos", map[string]any{"slug": org + "-r2", "name": "r2"}); st != http.StatusPaymentRequired {
		t.Fatalf("create repo when lapsed: want 402, got %d %s", st, b)
	}
	if st, b := apiCall(t, token, http.MethodGet, "/projects/"+org+"/repos/"+repo, nil); st != 200 {
		t.Fatalf("read repo when lapsed: %d %s", st, b)
	}
}
