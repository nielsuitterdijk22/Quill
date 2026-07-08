package auth_test

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nielsuitterdijk22/quill/internal/auth"
	"github.com/nielsuitterdijk22/quill/internal/config"
	"github.com/nielsuitterdijk22/quill/internal/store"
)

// newService spins up a Service against the live test database, or skips when
// QUILL_TEST_DATABASE_URL is unset (so CI without a DB stays green).
func newService(t *testing.T) *auth.Service {
	t.Helper()
	dsn := os.Getenv("QUILL_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("QUILL_TEST_DATABASE_URL not set; skipping auth integration test")
	}
	if err := store.Migrate(dsn); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	st, err := store.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(st.Close)
	// Start clean and clean up after: users cascade to auth_identities. Each
	// registration now also creates a per-account tenant (slug = username), so
	// clear those too — otherwise a reused username collides on tenants.slug
	// across runs. The seeded 'default' tenant is preserved.
	reset := func() {
		_, _ = st.Pool().Exec(context.Background(), "DELETE FROM users")
		_, _ = st.Pool().Exec(context.Background(), "DELETE FROM tenants WHERE slug <> 'default'")
	}
	reset()
	t.Cleanup(reset)
	tokens := auth.NewTokenService(config.JWTConfig{Secret: "itest-secret", Issuer: "quill", TTL: time.Hour})
	return auth.NewService(st, auth.NewLocalProvider(st), tokens)
}

func TestRegisterThenLogin(t *testing.T) {
	svc := newService(t)
	ctx := context.Background()

	id, err := svc.Register(ctx, auth.RegisterInput{
		Username: "alice",
		Email:    "alice@example.test",
		Password: "correct horse battery",
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	// First user becomes admin.
	if !id.IsAdmin {
		t.Fatalf("expected first user to be admin")
	}

	token, loginID, err := svc.Login(ctx, auth.Credentials{Username: "alice", Password: "correct horse battery"})
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if token == "" {
		t.Fatal("expected a token")
	}
	if loginID.UserID != id.UserID {
		t.Fatalf("login user mismatch: got %v want %v", loginID.UserID, id.UserID)
	}

	// Token must verify back to the same identity.
	verified, err := svc.Tokens().Verify(token)
	if err != nil {
		t.Fatalf("verify token: %v", err)
	}
	if verified.UserID != id.UserID {
		t.Fatalf("verified user mismatch")
	}
}

// TestRegisterGivesEachAccountItsOwnTenant is the core tenant-isolation
// guarantee: two accounts must land in different tenants (never the shared
// default), and that tenant must survive login and the JWT round-trip so every
// request resolves it.
func TestRegisterGivesEachAccountItsOwnTenant(t *testing.T) {
	svc := newService(t)
	ctx := context.Background()

	alice, err := svc.Register(ctx, auth.RegisterInput{
		Username: "alice", Email: "alice@example.test", Password: "correct horse battery",
	})
	if err != nil {
		t.Fatalf("register alice: %v", err)
	}
	bob, err := svc.Register(ctx, auth.RegisterInput{
		Username: "bob", Email: "bob@example.test", Password: "correct horse battery",
	})
	if err != nil {
		t.Fatalf("register bob: %v", err)
	}

	if alice.TenantID == (uuid.UUID{}) || bob.TenantID == (uuid.UUID{}) {
		t.Fatalf("each account must have a tenant: alice=%v bob=%v", alice.TenantID, bob.TenantID)
	}
	if alice.TenantID == bob.TenantID {
		t.Fatalf("tenant isolation violated: accounts share tenant %v", alice.TenantID)
	}

	// The tenant must flow through login and verify back out of the token.
	token, loginID, err := svc.Login(ctx, auth.Credentials{Username: "alice", Password: "correct horse battery"})
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if loginID.TenantID != alice.TenantID {
		t.Fatalf("login dropped tenant: got %v want %v", loginID.TenantID, alice.TenantID)
	}
	verified, err := svc.Tokens().Verify(token)
	if err != nil {
		t.Fatalf("verify token: %v", err)
	}
	if verified.TenantID != alice.TenantID {
		t.Fatalf("token dropped tenant: got %v want %v", verified.TenantID, alice.TenantID)
	}
}

func TestLoginIsCaseInsensitiveOnUsername(t *testing.T) {
	svc := newService(t)
	ctx := context.Background()

	if _, err := svc.Register(ctx, auth.RegisterInput{
		Username: "Bob",
		Email:    "bob@example.test",
		Password: "hunter2hunter2",
	}); err != nil {
		t.Fatalf("register: %v", err)
	}

	// Login with a differently-cased username must still work.
	if _, _, err := svc.Login(ctx, auth.Credentials{Username: "BOB", Password: "hunter2hunter2"}); err != nil {
		t.Fatalf("case-insensitive login failed: %v", err)
	}
}

func TestLoginRejectsWrongPassword(t *testing.T) {
	svc := newService(t)
	ctx := context.Background()

	if _, err := svc.Register(ctx, auth.RegisterInput{
		Username: "carol",
		Email:    "carol@example.test",
		Password: "rightpassword",
	}); err != nil {
		t.Fatalf("register: %v", err)
	}

	_, _, err := svc.Login(ctx, auth.Credentials{Username: "carol", Password: "wrongpassword"})
	if !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("expected ErrInvalidCredentials, got %v", err)
	}
}

func TestRegisterRejectsDuplicate(t *testing.T) {
	svc := newService(t)
	ctx := context.Background()

	in := auth.RegisterInput{Username: "dave", Email: "dave@example.test", Password: "password123"}
	if _, err := svc.Register(ctx, in); err != nil {
		t.Fatalf("first register: %v", err)
	}
	_, err := svc.Register(ctx, in)
	if !errors.Is(err, auth.ErrUserExists) {
		t.Fatalf("expected ErrUserExists, got %v", err)
	}
}

func TestRegisterValidatesInput(t *testing.T) {
	svc := newService(t)
	ctx := context.Background()

	cases := map[string]auth.RegisterInput{
		"bad username": {Username: "a", Email: "a@example.test", Password: "password123"},
		"bad email":    {Username: "validname", Email: "not-an-email", Password: "password123"},
		"short pass":   {Username: "validname", Email: "v@example.test", Password: "short"},
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := svc.Register(ctx, in); !errors.Is(err, auth.ErrInvalidInput) {
				t.Fatalf("expected ErrInvalidInput, got %v", err)
			}
		})
	}
}
