package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/lestrrat-go/jwx/v3/jwa"
	"github.com/lestrrat-go/jwx/v3/jwk"
	"github.com/lestrrat-go/jwx/v3/jwt"

	"github.com/nielsuitterdijk22/quill/internal/config"
)

// signToken mints a minimal Zitadel-shaped access token signed by priv.
func signToken(t *testing.T, priv *rsa.PrivateKey, issuer, kid, sub string) string {
	t.Helper()
	tok, err := jwt.NewBuilder().
		Issuer(issuer).
		Subject(sub).
		Audience([]string{issuer}).
		Expiration(time.Now().Add(time.Hour)).
		IssuedAt(time.Now()).
		Build()
	if err != nil {
		t.Fatalf("build token: %v", err)
	}
	key, err := jwk.Import(priv)
	if err != nil {
		t.Fatalf("jwk sign key: %v", err)
	}
	if err := key.Set(jwk.KeyIDKey, kid); err != nil {
		t.Fatalf("set kid: %v", err)
	}
	signed, err := jwt.Sign(tok, jwt.WithKey(jwa.RS256(), key))
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}
	return string(signed)
}

// setKidInCache reports whether the verifier's cached JWKS holds kid.
func setKidInCache(v *ZitadelVerifier, kid string) bool {
	v.mu.RLock()
	defer v.mu.RUnlock()
	for i := 0; i < v.keySet.Len(); i++ {
		k, _ := v.keySet.Key(i)
		var id string
		_ = k.Get(jwk.KeyIDKey, &id)
		if id == kid {
			return true
		}
	}
	return false
}

// TestZitadelVerifyRefetchesAfterKeyRotation pins the key-rotation recovery:
// a token signed by a NEW IdP key (minted after the verifier cached its JWKS)
// must trigger a live refetch instead of rejecting with stale keys.
//
// The verifier's store is nil: verification and refetch happen before identity
// resolution, and a recovered parse proceeds into identity resolution - the
// recoverable nil-store panic is irrelevant to the assertion, which is purely
// about the JWKS cache and fetch count.
func TestZitadelVerifyRefetchesAfterKeyRotation(t *testing.T) {
	oldPriv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("gen old key: %v", err)
	}
	newPriv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("gen new key: %v", err)
	}

	var serveMu sync.Mutex
	serving := oldPriv
	servingKid := "old-kid"
	fetches := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		serveMu.Lock()
		k := serving
		kid := servingKid
		fetches++
		serveMu.Unlock()
		pub, err := jwk.Import(&k.PublicKey)
		if err != nil {
			t.Errorf("jwk import: %v", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		if err := pub.Set(jwk.KeyIDKey, kid); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		set := jwk.NewSet()
		_ = set.AddKey(pub)
		_ = json.NewEncoder(w).Encode(set)
	}))
	defer server.Close()
	fetchCount := func() int {
		serveMu.Lock()
		defer serveMu.Unlock()
		return fetches
	}

	v := NewZitadelVerifier(config.ZitadelConfig{Issuer: server.URL}, nil, slog.New(slog.DiscardHandler))
	v.Start(context.Background())
	if fetchCount() == 0 {
		t.Fatalf("initial JWKS fetch missing")
	}
	if err := v.refresh(context.Background()); err != nil { // deterministic cache: only the old key
		t.Fatalf("prefetch: %v", err)
	}

	// Rotate the served key (as the IdP already did) and mint a fresh token
	// signed by the new key: the cached set cannot verify it.
	serveMu.Lock()
	serving = newPriv
	servingKid = "new-kid"
	serveMu.Unlock()
	token := signToken(t, newPriv, server.URL, "new-kid", "user-1")

	before := fetchCount()
	func() {
		defer func() { _ = recover() }() // nil store at resolveIdentity
		_, _ = v.Verify(context.Background(), token)
	}()

	if got := fetchCount(); got != before+1 {
		t.Fatalf("failed verification must trigger exactly one JWKS refetch: before=%d after=%d", before, got)
	}
	if !setKidInCache(v, "new-kid") {
		t.Fatalf("JWKS cache was not refreshed with the rotated key")
	}

	// Cooldown: an immediately following failure must NOT refetch again.
	before = fetchCount()
	if _, err := v.Verify(context.Background(), "not.a.jwt"); err == nil {
		t.Fatalf("garbage token should fail verification")
	}
	if fetchCount() != before {
		t.Fatalf("cooldown violated: refetch fired within a minute of the previous one")
	}
}
