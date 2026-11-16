package zitadel

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type recorded struct {
	Method, Path, Org string
	Body              map[string]any
}

// fakeZitadel answers like our Zitadel (shapes verified live 2026-11-16).
func fakeZitadel(t *testing.T, handle func(r recorded) (int, string)) (*Client, *[]recorded) {
	t.Helper()
	var calls []recorded
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer pat" {
			http.Error(w, "unauthenticated", http.StatusUnauthorized)
			return
		}
		rec := recorded{Method: r.Method, Path: r.URL.Path, Org: r.Header.Get("x-zitadel-orgid")}
		_ = json.NewDecoder(r.Body).Decode(&rec.Body)
		calls = append(calls, rec)
		st, body := handle(rec)
		w.WriteHeader(st)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return NewClient(srv.URL, "pat"), &calls
}

func TestSSOProviderCalls(t *testing.T) {
	c, calls := fakeZitadel(t, func(r recorded) (int, string) {
		switch {
		case r.Method == "POST" && r.Path == "/management/v1/idps/generic_oidc":
			return 200, `{"id":"idp-1"}`
		case r.Method == "POST" && r.Path == "/management/v1/idps/saml":
			return 200, `{"id":"idp-2"}`
		}
		return 200, `{}`
	})
	ctx := context.Background()

	id, err := c.UpsertOIDCProvider(ctx, "org-1", "", OIDCProvider{Name: "Acme SSO", Issuer: "https://login.acme.test", ClientID: "cid", ClientSecret: "sec"})
	if err != nil || id != "idp-1" {
		t.Fatalf("create oidc: %q %v", id, err)
	}
	got := (*calls)[0]
	if got.Org != "org-1" || got.Body["issuer"] != "https://login.acme.test" || got.Body["clientSecret"] != "sec" || got.Body["isIdTokenMapping"] != true {
		t.Fatalf("create oidc request = %+v", got)
	}
	opts, _ := got.Body["providerOptions"].(map[string]any)
	if opts["isAutoCreation"] != true || opts["isCreationAllowed"] != true || opts["isLinkingAllowed"] != true {
		t.Fatalf("provider options = %v", opts)
	}

	// Update without a secret keeps the stored one (no clientSecret sent).
	if _, err := c.UpsertOIDCProvider(ctx, "org-1", "idp-1", OIDCProvider{Name: "Acme SSO", Issuer: "https://login.acme.test", ClientID: "cid2"}); err != nil {
		t.Fatal(err)
	}
	upd := (*calls)[1]
	if upd.Method != "PUT" || upd.Path != "/management/v1/idps/generic_oidc/idp-1" || upd.Body["clientSecret"] != nil {
		t.Fatalf("update oidc request = %+v", upd)
	}

	xml := []byte("<md:EntityDescriptor/>")
	if id, err := c.UpsertSAMLProvider(ctx, "org-1", "", "Acme SAML", xml); err != nil || id != "idp-2" {
		t.Fatalf("create saml: %q %v", id, err)
	}
	saml := (*calls)[2]
	if saml.Body["metadataXml"] != base64.StdEncoding.EncodeToString(xml) || saml.Body["binding"] != "SAML_BINDING_POST" {
		t.Fatalf("saml request = %+v", saml)
	}
}

func TestSSOPolicyAndDomainIdempotency(t *testing.T) {
	policyExists := true
	c, calls := fakeZitadel(t, func(r recorded) (int, string) {
		switch {
		case r.Method == "POST" && r.Path == "/management/v1/policies/login" && policyExists:
			return 409, `{"code":6,"message":"Login Policy already exists"}`
		case r.Method == "POST" && r.Path == "/management/v1/policies/login/idps":
			return 409, `{"code":6,"message":"Errors.Org.LoginPolicy.IDP.AlreadyExists"}`
		case r.Method == "POST" && r.Path == "/management/v1/orgs/me/domains":
			return 409, `{"code":6,"message":"already exists"}`
		case r.Method == "DELETE":
			return 404, `{"code":5,"message":"not found"}`
		}
		return 200, `{}`
	})
	ctx := context.Background()
	if err := c.EnsureSSOLoginPolicy(ctx, "org-1", "idp-1"); err != nil {
		t.Fatalf("policy with existing custom policy + idp: %v", err)
	}
	first := (*calls)[0]
	idps, _ := first.Body["idps"].([]any)
	idp0, _ := idps[0].(map[string]any)
	if first.Body["allowUsernamePassword"] != false || first.Body["allowRegister"] != false || idp0["ownerType"] != "IDP_OWNER_TYPE_ORG" {
		t.Fatalf("policy body = %v", first.Body)
	}
	if (*calls)[1].Path != "/management/v1/policies/login/idps" {
		t.Fatalf("expected add-idp-to-policy fallback, got %+v", (*calls)[1])
	}
	if err := c.AddOrgDomain(ctx, "org-1", "acme.test"); err != nil {
		t.Fatalf("add existing domain: %v", err)
	}
	if err := c.RemoveOrgDomain(ctx, "org-1", "acme.test"); err != nil {
		t.Fatalf("remove missing domain: %v", err)
	}
	if err := c.DeleteProvider(ctx, "org-1", "idp-gone"); err != nil {
		t.Fatalf("delete missing idp: %v", err)
	}
	if err := c.DeleteOrg(ctx, "org-gone"); err != nil {
		t.Fatalf("delete missing org: %v", err)
	}
	if err := c.EnsureOrgScopedUsernames(ctx, "org-9"); err != nil {
		t.Fatalf("scoped usernames: %v", err)
	}
	dp := (*calls)[len(*calls)-1]
	if dp.Path != "/admin/v1/orgs/org-9/policies/domain" || dp.Body["userLoginMustBeDomain"] != true {
		t.Fatalf("domain policy request = %+v", dp)
	}
	last := (*calls)[len(*calls)-2]
	if last.Path != "/management/v1/orgs/me" || last.Org != "org-gone" {
		t.Fatalf("delete org request = %+v", last)
	}

	// Other failures surface.
	policyExists = false
	c2, _ := fakeZitadel(t, func(r recorded) (int, string) { return 400, `{"code":3,"message":"invalid"}` })
	if err := c2.EnsureSSOLoginPolicy(ctx, "o", "i"); err == nil || !strings.Contains(err.Error(), "400") {
		t.Fatalf("400 must surface: %v", err)
	}
}
