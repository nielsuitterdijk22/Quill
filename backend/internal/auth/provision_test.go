package auth

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

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
	if tenant.Plan != "inactive" {
		t.Fatalf("tenant plan = %q, want inactive", tenant.Plan)
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

// TestSSOSignInJoinsOrgAndLinksVerifiedDomain: a sign-in whose Zitadel account
// lives in an org's SSO identity org joins that Quill org (roles untouched),
// and a first SSO sign-in claims the existing account with the same email —
// only on the org's verified domain, never for other domains or when SSO is
// off.
func TestSSOSignInJoinsOrgAndLinksVerifiedDomain(t *testing.T) {
	st := provisionTestStore(t)
	v := NewZitadelVerifier(config.ZitadelConfig{Issuer: "https://auth.example.test"}, st, slog.Default())
	ctx := context.Background()

	tenant, err := st.CreateOrgTenant(ctx, db.CreateOrgTenantParams{Slug: "ssoco", Name: "SSO Co"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.UpsertTenantSSO(ctx, db.UpsertTenantSSOParams{TenantID: tenant.ID, Protocol: "oidc", Issuer: "https://login.ssoco.test", ClientID: "c", EmailDomain: "ssoco.test", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if err := st.SetSSOState(ctx, tenant.ID, store.SSOState{ZitadelOrgID: "zorg-ssoco", ZitadelIdPID: "idp", DomainVerifyToken: "tok", DomainVerifiedAt: &now, RoutedDomain: "ssoco.test"}); err != nil {
		t.Fatal(err)
	}

	// An existing account (signed up with a password before SSO).
	existing, err := v.provisionUser(ctx, "sub-password", zitadelUserInfo{Email: "dana@ssoco.test", EmailVerified: true, PreferredUsername: "dana"})
	if err != nil {
		t.Fatal(err)
	}

	// Through a non-SSO org (or unverified email / other domain): no link.
	for _, c := range []struct {
		org  string
		info zitadelUserInfo
	}{
		{"zorg-other", zitadelUserInfo{Email: "dana@ssoco.test", EmailVerified: true}},
		{"zorg-ssoco", zitadelUserInfo{Email: "dana@ssoco.test", EmailVerified: false}},
		{"zorg-ssoco", zitadelUserInfo{Email: "dana@elsewhere.test", EmailVerified: true}},
	} {
		if _, ok, err := v.linkSSOAccount(ctx, "sub-sso", c.org, c.info); err != nil || ok {
			t.Fatalf("link %+v: ok=%v err=%v", c, ok, err)
		}
	}

	// The company IdP on the verified domain claims the account.
	id, ok, err := v.linkSSOAccount(ctx, "sub-sso", "zorg-ssoco", zitadelUserInfo{Email: "Dana@SSOCo.test", EmailVerified: true})
	if err != nil || !ok || id.UserID != existing.UserID || id.TenantID != existing.TenantID {
		t.Fatalf("sso link: %+v ok=%v err=%v (want user %s)", id, ok, err, existing.UserID)
	}
	idents, _ := st.ListAuthIdentitiesForUser(ctx, existing.UserID)
	if len(idents) != 2 {
		t.Fatalf("identities = %d, want password + sso", len(idents))
	}

	// Joining: SSO org sign-in adds membership; existing roles stay.
	v.joinSSOOrg(ctx, existing.UserID, "zorg-ssoco")
	m, err := st.GetTenantMember(ctx, db.GetTenantMemberParams{TenantID: tenant.ID, UserID: existing.UserID})
	if err != nil || m.Role != "member" {
		t.Fatalf("membership after sso sign-in: %+v err=%v", m, err)
	}
	if err := st.AddTenantMember(ctx, db.AddTenantMemberParams{TenantID: tenant.ID, UserID: existing.UserID, Role: "admin"}); err != nil {
		t.Fatal(err)
	}
	v.joinSSOOrg(ctx, existing.UserID, "zorg-ssoco")
	if m, _ := st.GetTenantMember(ctx, db.GetTenantMemberParams{TenantID: tenant.ID, UserID: existing.UserID}); m.Role != "admin" {
		t.Fatalf("role changed to %q", m.Role)
	}
	// Other orgs' sign-ins join nothing; disabled SSO joins nothing.
	other, _ := v.provisionUser(ctx, "sub-other", zitadelUserInfo{Email: "eve@other.test", EmailVerified: true, PreferredUsername: "eve"})
	v.joinSSOOrg(ctx, other.UserID, "zorg-other")
	if _, err := st.GetTenantMember(ctx, db.GetTenantMemberParams{TenantID: tenant.ID, UserID: other.UserID}); err == nil {
		t.Fatal("non-SSO sign-in joined the org")
	}
	if _, err := st.UpsertTenantSSO(ctx, db.UpsertTenantSSOParams{TenantID: tenant.ID, Protocol: "oidc", Issuer: "https://login.ssoco.test", ClientID: "c", EmailDomain: "ssoco.test", Enabled: false}); err != nil {
		t.Fatal(err)
	}
	v.joinSSOOrg(ctx, other.UserID, "zorg-ssoco")
	if _, err := st.GetTenantMember(ctx, db.GetTenantMemberParams{TenantID: tenant.ID, UserID: other.UserID}); err == nil {
		t.Fatal("disabled SSO still joins")
	}
}
