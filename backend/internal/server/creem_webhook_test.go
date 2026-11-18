package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/nielsuitterdijk22/quill/internal/config"
	"github.com/nielsuitterdijk22/quill/internal/creem"
	"github.com/nielsuitterdijk22/quill/internal/logging"
	"github.com/nielsuitterdijk22/quill/internal/store"
	"github.com/nielsuitterdijk22/quill/internal/store/db"
)

// TestCreemWebhookEndpoint drives POST /api/v1/webhooks/creem end-to-end with
// Creem's real wire format: the signature travels in the creem-signature
// header as hex HMAC-SHA256 of the raw body. A bad signature is a 400 (Creem
// shows it as failed), a good one upgrades the org's plan and answers 200.
func TestCreemWebhookEndpoint(t *testing.T) {
	dsn := os.Getenv("QUILL_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("QUILL_TEST_DATABASE_URL not set; skipping creem webhook integration test")
	}
	if err := store.Migrate(dsn); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	st, err := store.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(st.Close)
	ctx := context.Background()
	_, _ = st.Pool().Exec(ctx, `DELETE FROM tenants WHERE slug = 'creem-hook-org'`)
	tenant, err := st.CreateOrgTenant(ctx, db.CreateOrgTenantParams{Slug: "creem-hook-org", Name: "Creem Hook"})
	if err != nil {
		t.Fatalf("create tenant: %v", err)
	}
	t.Cleanup(func() { _, _ = st.Pool().Exec(context.Background(), `DELETE FROM tenants WHERE id = $1`, tenant.ID) })

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	cfg.Creem = creem.Config{APIKey: "sk_test", WebhookSecret: "whsec_srv", TeamProductID: "prod_team"}
	srv := New(cfg, logging.New("error", "text"), st)

	body := `{"id":"evt_1","eventType":"checkout.completed","object":{"id":"ch_1","object":"checkout","product":{"id":"prod_team"},"customer":{"id":"cust_1"},"subscription":{"id":"sub_1"},"metadata":{"tenantSlug":"creem-hook-org"}}}`
	post := func(sig string) int {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/creem", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if sig != "" {
			req.Header.Set("creem-signature", sig)
		}
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		return rec.Code
	}

	if code := post(""); code != http.StatusBadRequest {
		t.Fatalf("unsigned: want 400, got %d", code)
	}
	if code := post(creem.Sign([]byte("other"), []byte(body))); code != http.StatusBadRequest {
		t.Fatalf("wrong secret: want 400, got %d", code)
	}
	if code := post(creem.Sign([]byte("whsec_srv"), []byte(body))); code != http.StatusOK {
		t.Fatalf("signed: want 200, got %d", code)
	}
	got, err := st.GetTenantByID(ctx, tenant.ID)
	if err != nil || got.Plan != "team" {
		t.Fatalf("plan after webhook = %q err=%v", got.Plan, err)
	}
}
