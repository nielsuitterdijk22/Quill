package auth

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"testing"

	"github.com/nielsuitterdijk22/quill/internal/config"
)

// TestZitadelVerifierLive checks the verifier against a real Zitadel instance:
// JWKS fetch over the wire and rejection of non-JWKS-signed tokens. Gated on
// QUILL_TEST_ZITADEL_ISSUER (e.g. http://localhost:8081 from deploy/spike-zitadel)
// so CI without an instance stays green. It does not provision (store is nil),
// so it only exercises the pre-store verification path.
func TestZitadelVerifierLive(t *testing.T) {
	issuer := os.Getenv("QUILL_TEST_ZITADEL_ISSUER")
	if issuer == "" {
		t.Skip("QUILL_TEST_ZITADEL_ISSUER not set; skipping live Zitadel test")
	}
	v := NewZitadelVerifier(config.ZitadelConfig{Issuer: issuer}, nil, slog.Default())

	// Start performs the initial JWKS fetch synchronously; a failure here would
	// surface as a later verification failure, so assert connectivity directly.
	v.Start(context.Background())
	v.mu.RLock()
	ks := v.keySet
	v.mu.RUnlock()
	if ks == nil || ks.Len() == 0 {
		t.Fatalf("expected a non-empty JWKS from %s", issuer)
	}

	// A token not signed by the instance's keys must be rejected before any
	// store access (so nil store is safe here).
	if _, err := v.Verify(context.Background(), "not.a.jwt"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("garbage token: want ErrInvalidCredentials, got %v", err)
	}
}
