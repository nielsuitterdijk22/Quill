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

	// The fast path must resolve the caller's tenant from the user row —
	// the per-account tenant — not from the IdP org claims, which map to the
	// shared default tenant in a single-org deployment. Otherwise projects the
	// user creates land on the shared tenant and the free gates never meter.
	idFast, err := v.resolveIdentity(ctx, "unused", "sub-abc-123", "", "")
	if err != nil {
		t.Fatalf("resolveIdentity (fast path): %v", err)
	}
	if idFast.TenantID != tenant.ID {
		t.Fatalf("resolveIdentity tenant = %s, want per-account tenant %s", idFast.TenantID, tenant.ID)
	}
	if idFast.UserID != id.UserID {
		t.Fatalf("resolveIdentity user = %s, want %s", idFast.UserID, id.UserID)
	}
}

// TestProvisionedAccountLinksOnFirstSignIn: an account an org created over
// SCIM (no sign-in identity yet) is claimed by the person's first Zitadel
// sign-in with that verified email — and never otherwise: an unverified
// email, or an account that already has an identity, is not linked.
func TestProvisionedAccountLinksOnFirstSignIn(t *testing.T) {
	st := provisionTestStore(t)
	v := NewZitadelVerifier(config.ZitadelConfig{Issuer: "https://auth.example.test"}, st, slog.Default())
	ctx := context.Background()

	provisioned, err := st.CreateUser(ctx, db.CreateUserParams{Username: "bob", Email: "bob@acme.test", DisplayName: "Bob", IsActive: true})
	if err != nil {
		t.Fatal(err)
	}
	// An org already owns the slug "bob": the account tenant must not collide.
	if _, err := st.CreateOrgTenant(ctx, db.CreateOrgTenantParams{Slug: "bob", Name: "Bob's Org"}); err != nil {
		t.Fatal(err)
	}

	// Unverified email: not linked (and the duplicate email can't be created).
	if _, err := v.provisionUser(ctx, "sub-unverified", zitadelUserInfo{Email: "bob@acme.test", PreferredUsername: "bob"}); err == nil {
		t.Fatal("unverified email must not claim a provisioned account")
	}
	if idents, _ := st.ListAuthIdentitiesForUser(ctx, provisioned.ID); len(idents) != 0 {
		t.Fatalf("unverified sign-in attached an identity: %v", idents)
	}

	// Verified: linked to the same account, which gets its own tenant.
	id, err := v.provisionUser(ctx, "sub-bob", zitadelUserInfo{Email: "Bob@Acme.test", EmailVerified: true, PreferredUsername: "bob"})
	if err != nil {
		t.Fatalf("link: %v", err)
	}
	if id.UserID != provisioned.ID || id.Username != "bob" {
		t.Fatalf("linked identity = %+v, want user %s", id, provisioned.ID)
	}
	tenant, err := st.GetTenantByID(ctx, id.TenantID)
	if err != nil || tenant.Slug == "bob" || tenant.Slug == "" {
		t.Fatalf("account tenant = %+v err=%v (must not reuse the org's slug)", tenant, err)
	}
	ident, err := st.GetAuthIdentity(ctx, db.GetAuthIdentityParams{Provider: ProviderZitadel, Subject: "sub-bob"})
	if err != nil || ident.UserID != provisioned.ID {
		t.Fatalf("identity not recorded: %+v err=%v", ident, err)
	}

	// A second Zitadel subject with the same verified email can't take the
	// account over now that it has an identity.
	if _, err := v.provisionUser(ctx, "sub-attacker", zitadelUserInfo{Email: "bob@acme.test", EmailVerified: true, PreferredUsername: "bob"}); err == nil {
		t.Fatal("an account with an identity must never be linked again")
	}
	if _, err := st.GetAuthIdentity(ctx, db.GetAuthIdentityParams{Provider: ProviderZitadel, Subject: "sub-attacker"}); err == nil {
		t.Fatal("attacker identity recorded")
	}
}
