package auth

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nielsuitterdijk22/quill/internal/config"
)

func TestTokenIssueVerifyRoundTrip(t *testing.T) {
	ts := NewTokenService(config.JWTConfig{Secret: "test-secret", Issuer: "quill", TTL: time.Hour})
	want := Identity{UserID: uuid.New(), Username: "alice", Email: "alice@example.test", IsAdmin: true}

	token, exp, err := ts.Issue(want)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if token == "" {
		t.Fatal("empty token")
	}
	if time.Until(exp) <= 0 {
		t.Fatalf("expiry not in the future: %v", exp)
	}

	got, err := ts.Verify(token)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if got != want {
		t.Fatalf("identity mismatch: got %+v want %+v", got, want)
	}
}

func TestTokenVerifyRejectsTampered(t *testing.T) {
	ts := NewTokenService(config.JWTConfig{Secret: "test-secret", Issuer: "quill", TTL: time.Hour})
	token, _, err := ts.Issue(Identity{UserID: uuid.New(), Username: "bob"})
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if _, err := ts.Verify(token + "x"); err == nil {
		t.Fatal("expected tampered token to be rejected")
	}
}

func TestTokenVerifyRejectsWrongSecret(t *testing.T) {
	a := NewTokenService(config.JWTConfig{Secret: "secret-a", Issuer: "quill", TTL: time.Hour})
	b := NewTokenService(config.JWTConfig{Secret: "secret-b", Issuer: "quill", TTL: time.Hour})
	token, _, err := a.Issue(Identity{UserID: uuid.New(), Username: "carol"})
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if _, err := b.Verify(token); err == nil {
		t.Fatal("expected token signed with a different secret to be rejected")
	}
}

func TestTokenVerifyRejectsExpired(t *testing.T) {
	ts := NewTokenService(config.JWTConfig{Secret: "test-secret", Issuer: "quill", TTL: time.Millisecond})
	token, _, err := ts.Issue(Identity{UserID: uuid.New(), Username: "dave"})
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	time.Sleep(10 * time.Millisecond)
	if _, err := ts.Verify(token); err == nil {
		t.Fatal("expected expired token to be rejected")
	}
}
