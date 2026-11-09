package auth

import (
	"context"
	"log/slog"
	"os"
	"testing"

	"github.com/nielsuitterdijk22/quill/internal/config"
	"github.com/nielsuitterdijk22/quill/internal/store"
	"github.com/nielsuitterdijk22/quill/internal/store/db"
)

// provisionTestStore opens the scratch database (gated on
// QUILL_TEST_DATABASE_URL so CI without a DB stays green).
func provisionTestStore(t *testing.T) *store.Store {
	t.Helper()
	dsn := os.Getenv("QUILL_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("QUILL_TEST_DATABASE_URL not set; skipping provisioning test")
	}
	if err := store.Migrate(dsn); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	st, err := store.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(st.Close)
	reset := func() {
		_, _ = st.Pool().Exec(context.Background(), "DELETE FROM users")
		_, _ = st.Pool().Exec(context.Background(), "DELETE FROM tenants WHERE slug <> 'default'")
	}
	reset()
	t.Cleanup(reset)
	return st
}

// TestProvisionUserCreatesPersonalTenant: a first-time Zitadel login now
// provisions a per-account tenant (freemium gating per individual + the target
// of the personal Pro checkout). The local register path already did this
// (auth/service.go); this pins the Zitadel side to the same contract.
func TestProvisionUserCreatesPersonalTenant(t *testing.T) {
	st := provisionTestStore(t)
	v := NewZitadelVerifier(config.ZitadelConfig{Issuer: "https://auth.example.test"}, st, slog.Default())
	ctx := context.Background()

	id, err := v.provisionUser(ctx, "sub-abc-123", zitadelUserInfo{
		Email:             "alice@example.test",
		PreferredUsername: "alice",
		Name:              "Alice Doe",
	})
	if err != nil {
		t.Fatalf("provisionUser: %v", err)
	}

	// The identity carries the personal tenant…
	tenant, err := st.GetTenantByID(ctx, id.TenantID)
	if err != nil {
		t.Fatalf("identity tenant missing: %v", err)
	}
	// …whose kind is personal, slug mirrors the username, and plan starts on
	// free (freemium migration 000021's default). kind is asserted via raw SQL
	// because the committed sqlc Tenant model doesn't project it (a known
	// generated-code drift).
	var kind string
	if err := st.Pool().QueryRow(ctx, "SELECT kind FROM tenants WHERE id = $1", tenant.ID).Scan(&kind); err != nil {
		t.Fatalf("tenant kind lookup: %v", err)
	}
	if kind != "personal" {
		t.Fatalf("tenant kind = %q, want personal", kind)
	}
	if tenant.Slug != "alice" {
		t.Fatalf("tenant slug = %q, want alice", tenant.Slug)
	}
	if tenant.Plan != "free" {
		t.Fatalf("tenant plan = %q, want free", tenant.Plan)
	}

	// The user row is linked to the tenant, so personal projects attach to it.
	user, err := st.GetUserByID(ctx, id.UserID)
	if err != nil {
		t.Fatalf("lookup user: %v", err)
	}
	if !user.TenantID.Valid || user.TenantID.UUID != tenant.ID {
		t.Fatalf("user tenant link = %+v, want %s", user.TenantID, tenant.ID)
	}

	// Idempotency is the fast path in resolveIdentity: the auth identity
	// lookup by subject short-circuits provisioning, so a second login never
	// reaches provisionUser.
	ident, err := st.GetAuthIdentity(ctx, db.GetAuthIdentityParams{
		Provider: ProviderZitadel,
		Subject:  "sub-abc-123",
	})
	if err != nil {
		t.Fatalf("auth identity lookup after provisioning: %v", err)
	}
	if ident.UserID != id.UserID {
		t.Fatalf("auth identity user = %s, want %s", ident.UserID, id.UserID)
	}
}
