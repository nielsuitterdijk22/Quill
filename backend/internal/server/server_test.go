package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nielsuitterdijk22/quill/internal/config"
	"github.com/nielsuitterdijk22/quill/internal/logging"
)

func newTestServer(t *testing.T) *Server {
	t.Helper()
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	return New(cfg, logging.New("error", "text"), nil)
}

func TestHealthz(t *testing.T) {
	srv := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestMeta(t *testing.T) {
	srv := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/meta", nil)
	rec := httptest.NewRecorder()

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json; charset=utf-8" {
		t.Fatalf("unexpected content-type: %q", ct)
	}
}

// TestReadyzReflectsIdPHealth pins the sign-in-loop self-heal: with an
// external IdP configured but its JWKS not loaded (the wedged-instance
// state), readiness fails so the platform restarts the container; without
// an external IdP readiness is unaffected.
func TestReadyzReflectsIdPHealth(t *testing.T) {
	// No Zitadel configured: ready.
	srv := newTestServer(t)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("readyz without IdP: %d, want 200", rec.Code)
	}

	// Zitadel configured but never started (no JWKS): not ready.
	t.Setenv("ZITADEL_ISSUER", "https://auth.example.test")
	srv = newTestServer(t)
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("readyz with unloaded JWKS: %d, want 503", rec.Code)
	}
}
