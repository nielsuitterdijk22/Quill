package platform

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/nielsuitterdijk22/quill/internal/zitadel"
)

// fakeSSO records what Quill applies to the identity provider.
type fakeSSO struct {
	mu       sync.Mutex
	orgs     map[string]bool
	idps     map[string]string // id -> "oidc:<issuer>:<clientId>:<secret>" | "saml:<len>"
	policies map[string]string // org -> idp on its login policy
	domains  map[string]map[string]bool
	n        int
	failNext string // method name to fail once
	scoped   []string
}

func newFakeSSO() *fakeSSO {
	return &fakeSSO{orgs: map[string]bool{}, idps: map[string]string{}, policies: map[string]string{}, domains: map[string]map[string]bool{}}
}

func (f *fakeSSO) fail(m string) error {
	if f.failNext == m {
		f.failNext = ""
		return errors.New("boom")
	}
	return nil
}
func (f *fakeSSO) Enabled() bool  { return true }
func (f *fakeSSO) Issuer() string { return "https://auth.example.test" }
func (f *fakeSSO) CreateOrg(_ context.Context, name string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.fail("CreateOrg"); err != nil {
		return "", err
	}
	f.n++
	id := fmt.Sprintf("zorg-%d", f.n)
	f.orgs[id] = true
	return id, nil
}
func (f *fakeSSO) EnsureOrgScopedUsernames(_ context.Context, orgID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.orgs[orgID] {
		return errors.New("unknown org")
	}
	f.scoped = append(f.scoped, orgID)
	return nil
}
func (f *fakeSSO) UpsertOIDCProvider(_ context.Context, orgID, idpID string, p zitadel.OIDCProvider) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.fail("UpsertOIDCProvider"); err != nil {
		return "", err
	}
	if idpID == "" {
		f.n++
		idpID = fmt.Sprintf("idp-%d", f.n)
		if p.ClientSecret == "" {
			return "", errors.New("secret required on create")
		}
	}
	secret := p.ClientSecret
	if secret == "" {
		secret = strings.Split(f.idps[idpID], ":")[len(strings.Split(f.idps[idpID], ":"))-1]
	}
	f.idps[idpID] = "oidc:" + p.Issuer + ":" + p.ClientID + ":" + secret
	return idpID, nil
}
func (f *fakeSSO) UpsertSAMLProvider(_ context.Context, orgID, idpID, name string, xml []byte) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if idpID == "" {
		f.n++
		idpID = fmt.Sprintf("idp-%d", f.n)
	}
	f.idps[idpID] = fmt.Sprintf("saml:%d", len(xml))
	return idpID, nil
}
func (f *fakeSSO) DeleteProvider(_ context.Context, orgID, idpID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.idps, idpID)
	if f.policies[orgID] == idpID {
		delete(f.policies, orgID)
	}
	return nil
}
func (f *fakeSSO) EnsureSSOLoginPolicy(_ context.Context, orgID, idpID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.policies[orgID] = idpID
	return nil
}
func (f *fakeSSO) AddOrgDomain(_ context.Context, orgID, domain string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.domains[orgID] == nil {
		f.domains[orgID] = map[string]bool{}
	}
	f.domains[orgID][domain] = true
	return nil
}
func (f *fakeSSO) RemoveOrgDomain(_ context.Context, orgID, domain string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.domains[orgID], domain)
	return nil
}
func (f *fakeSSO) DeleteOrg(_ context.Context, orgID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.orgs, orgID)
	return nil
}
func (f *fakeSSO) routed() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for org, ds := range f.domains {
		for d := range ds {
			out = append(out, org+"/"+d)
		}
	}
	sort.Strings(out)
	return out
}

// TestSSOLoginLifecycle: enabling SSO creates the identity org, the IdP and
// an SSO-only login policy; the email domain routes sign-ins only after the
// DNS TXT record proves ownership; disabling, changing the domain and
// removing SSO stop routing at once.
func TestSSOLoginLifecycle(t *testing.T) {
	svc, st := scopeTestService(t)
	ctx := context.Background()
	fz := newFakeSSO()
	svc.WithSSO(fz)
	txt := map[string][]string{}
	svc.txtLookup = func(_ context.Context, name string) ([]string, error) { return txt[name], nil }

	admin := Actor{UserID: scopeMakeUser(t, st, "sso-admin")}
	if _, _, err := svc.CreateOrganization(ctx, admin, "ssoorg", "SSO Org"); err != nil {
		t.Fatal(err)
	}
	activateOrg(t, svc, "ssoorg", PlanTeam)
	in := SSOConfigInput{Protocol: "oidc", Issuer: "https://login.acme.test/", ClientID: "cid", ClientSecret: "s3cret", EmailDomain: "Acme.test", Enabled: true}
	v, err := svc.SetOrgSSO(ctx, admin, "ssoorg", in)
	if err != nil {
		t.Fatalf("enable: %v", err)
	}
	if !v.Active || v.LastError != "" || v.ZitadelOrgID == "" || v.Routing || v.DomainVerified {
		t.Fatalf("after enable: %+v", v)
	}
	if v.Issuer != "https://login.acme.test" || v.EmailDomain != "acme.test" {
		t.Fatalf("normalization: %+v", v)
	}
	if v.RedirectURI != "https://auth.example.test/ui/login/login/externalidp/callback" {
		t.Fatalf("redirect uri = %q", v.RedirectURI)
	}
	if v.VerifyRecordName != "_quill-verification.acme.test" || !strings.HasPrefix(v.VerifyRecordValue, "quill-verification=") {
		t.Fatalf("verify record = %q %q", v.VerifyRecordName, v.VerifyRecordValue)
	}
	org := v.ZitadelOrgID
	if len(fz.scoped) == 0 || fz.scoped[0] != org {
		t.Fatalf("identity org usernames not scoped: %v", fz.scoped)
	}
	idp := fz.policies[org]
	if idp == "" || fz.idps[idp] != "oidc:https://login.acme.test:cid:s3cret" {
		t.Fatalf("idp/policy not applied: policies=%v idps=%v", fz.policies, fz.idps)
	}
	if len(fz.routed()) != 0 {
		t.Fatalf("unverified domain is routing: %v", fz.routed())
	}

	// Verification fails until the TXT record exists, and with a wrong value.
	if _, err := svc.VerifyOrgSSODomain(ctx, admin, "ssoorg"); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("verify without record: %v", err)
	}
	txt[v.VerifyRecordName] = []string{"quill-verification=wrong"}
	if _, err := svc.VerifyOrgSSODomain(ctx, admin, "ssoorg"); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("verify with wrong record: %v", err)
	}
	txt[v.VerifyRecordName] = []string{"v=spf1 -all", v.VerifyRecordValue}
	v, err = svc.VerifyOrgSSODomain(ctx, admin, "ssoorg")
	if err != nil || !v.DomainVerified || !v.Routing {
		t.Fatalf("verify: %+v err=%v", v, err)
	}
	if got := fz.routed(); len(got) != 1 || got[0] != org+"/acme.test" {
		t.Fatalf("routing = %v", got)
	}

	// Saving again (secret blank) keeps the secret and the same org/IdP.
	in.ClientSecret = ""
	in.ClientID = "cid2"
	v, err = svc.SetOrgSSO(ctx, admin, "ssoorg", in)
	if err != nil || v.ZitadelOrgID != org || fz.idps[idp] != "oidc:https://login.acme.test:cid2:s3cret" || !v.Routing {
		t.Fatalf("re-save: %+v idps=%v err=%v", v, fz.idps, err)
	}

	// Another org can't claim the verified domain.
	other := Actor{UserID: scopeMakeUser(t, st, "sso-other")}
	if _, _, err := svc.CreateOrganization(ctx, other, "ssoother", "Other"); err != nil {
		t.Fatal(err)
	}
	activateOrg(t, svc, "ssoother", PlanTeam)
	ov, err := svc.SetOrgSSO(ctx, other, "ssoother", SSOConfigInput{Protocol: "oidc", Issuer: "https://evil.test", ClientID: "x", ClientSecret: "y", EmailDomain: "acme.test", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	txt[ov.VerifyRecordName] = append(txt[ov.VerifyRecordName], ov.VerifyRecordValue)
	if _, err := svc.VerifyOrgSSODomain(ctx, other, "ssoother"); !errors.Is(err, ErrConflict) {
		t.Fatalf("second org verifying a taken domain: %v", err)
	}
	if got := fz.routed(); len(got) != 1 {
		t.Fatalf("routing after hijack attempt = %v", got)
	}

	// Disabling stops routing but keeps verification.
	in.Enabled = false
	v, _ = svc.SetOrgSSO(ctx, admin, "ssoorg", in)
	if v.Routing || !v.DomainVerified || len(fz.routed()) != 0 {
		t.Fatalf("disable: %+v routed=%v", v, fz.routed())
	}
	// Re-enabling routes again without re-verifying.
	in.Enabled = true
	v, _ = svc.SetOrgSSO(ctx, admin, "ssoorg", in)
	if !v.Routing || len(fz.routed()) != 1 {
		t.Fatalf("re-enable: %+v routed=%v", v, fz.routed())
	}
	// Changing the domain stops routing the old one and needs a new proof.
	in.EmailDomain = "acme-corp.test"
	v, _ = svc.SetOrgSSO(ctx, admin, "ssoorg", in)
	if v.Routing || v.DomainVerified || len(fz.routed()) != 0 || v.VerifyRecordName != "_quill-verification.acme-corp.test" {
		t.Fatalf("domain change: %+v routed=%v", v, fz.routed())
	}

	// Switching to SAML replaces the IdP (metadata fetched by Quill).
	svc.samlFetch = func(_ context.Context, u string) ([]byte, error) {
		if u != "https://idp.acme.test/metadata" {
			return nil, fmt.Errorf("unexpected url %s", u)
		}
		return []byte("<EntityDescriptor/>"), nil
	}
	v, err = svc.SetOrgSSO(ctx, admin, "ssoorg", SSOConfigInput{Protocol: "saml", Issuer: "https://idp.acme.test/metadata", EmailDomain: "acme-corp.test", Enabled: true})
	if err != nil || !v.Active || v.SAMLACSURL == "" || v.SAMLEntityID == "" {
		t.Fatalf("saml: %+v err=%v", v, err)
	}
	if _, still := fz.idps[idp]; still {
		t.Fatal("old OIDC provider not removed")
	}
	if fz.idps[fz.policies[org]] != "saml:19" {
		t.Fatalf("saml idp = %v", fz.idps)
	}

	// Removing SSO tears the IdP down and stops routing.
	if err := svc.DeleteOrgSSO(ctx, admin, "ssoorg"); err != nil {
		t.Fatal(err)
	}
	if len(fz.idps) != 1 || len(fz.routed()) != 0 { // only the other org's IdP remains
		t.Fatalf("after delete: idps=%v routed=%v", fz.idps, fz.routed())
	}
	if gv, _ := svc.GetOrgSSO(ctx, admin, "ssoorg"); gv.Configured {
		t.Fatalf("removed SSO still reported configured: %+v", gv)
	}
	// Setting SSO up again reuses the identity org (its accounts stay ours).
	in = SSOConfigInput{Protocol: "oidc", Issuer: "https://login.acme.test", ClientID: "cid", ClientSecret: "s3cret", Enabled: true}
	v, err = svc.SetOrgSSO(ctx, admin, "ssoorg", in)
	if err != nil || v.ZitadelOrgID != org || !v.Active {
		t.Fatalf("re-setup: %+v err=%v (want identity org %s)", v, err, org)
	}
}

// TestSSORules: public mailbox domains, plain-http issuers, missing secrets
// and orgs without an active plan are refused; failures are recorded rather
// than lost, and a retry converges.
func TestSSORules(t *testing.T) {
	svc, st := scopeTestService(t)
	ctx := context.Background()
	fz := newFakeSSO()
	svc.WithSSO(fz)

	admin := Actor{UserID: scopeMakeUser(t, st, "rules-admin")}
	if _, _, err := svc.CreateOrganization(ctx, admin, "rulesorg", "Rules"); err != nil {
		t.Fatal(err)
	}
	activateOrg(t, svc, "rulesorg", PlanTeam)
	base := SSOConfigInput{Protocol: "oidc", Issuer: "https://login.rules.test", ClientID: "c", ClientSecret: "s", EmailDomain: "rules.test", Enabled: true}
	for name, mut := range map[string]func(*SSOConfigInput){
		"public domain": func(i *SSOConfigInput) { i.EmailDomain = "gmail.com" },
		"http issuer":   func(i *SSOConfigInput) { i.Issuer = "http://login.rules.test" },
		"no secret":     func(i *SSOConfigInput) { i.ClientSecret = "" },
		"bad protocol":  func(i *SSOConfigInput) { i.Protocol = "ldap" },
	} {
		in := base
		mut(&in)
		if _, err := svc.SetOrgSSO(ctx, admin, "rulesorg", in); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("%s: want ErrInvalidInput, got %v", name, err)
		}
	}

	// A failing IdP call is recorded; saving again retries and converges on
	// the same identity org.
	fz.failNext = "UpsertOIDCProvider"
	v, err := svc.SetOrgSSO(ctx, admin, "rulesorg", base)
	if err != nil || v.Active || !strings.Contains(v.LastError, "configure OIDC provider") || v.ZitadelOrgID == "" {
		t.Fatalf("failed apply: %+v err=%v", v, err)
	}
	org := v.ZitadelOrgID
	base.ClientSecret = ""
	v, err = svc.SetOrgSSO(ctx, admin, "rulesorg", base)
	if err != nil || !v.Active || v.LastError != "" || v.ZitadelOrgID != org || len(fz.orgs) != 1 {
		t.Fatalf("retry: %+v orgs=%v err=%v", v, fz.orgs, err)
	}

	// Members can't read or change SSO.
	member := Actor{UserID: scopeMakeUser(t, st, "rules-member")}
	if _, err := svc.GetOrgSSO(ctx, member, "rulesorg"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("member read: %v", err)
	}

	// A lapsed org can't enable SSO.
	if _, err := svc.SetTenantPlan(ctx, Actor{IsAdmin: true}, "rulesorg", PlanLapsed); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetOrgSSO(ctx, admin, "rulesorg", base); !errors.Is(err, ErrLimitReached) {
		t.Fatalf("lapsed enable: %v", err)
	}
}
