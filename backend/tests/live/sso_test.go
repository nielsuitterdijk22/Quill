package live

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"
)

// TestSSOLogin drives org single sign-on end to end against the deployed
// platform, with our own Zitadel acting as the "company IdP" (a dedicated
// OIDC app + user in the Quill org; Secret Manager app-e2e-sso-upstream):
// an org admin configures OIDC SSO, a person signs in through the org's
// identity org (org-scoped authorize, since a test can't publish the DNS
// record domain discovery needs), is bounced to the upstream IdP, comes back
// with a JIT-created account, and is a member of the Quill org.
//
// Env: QUILL_E2E_SSO_UPSTREAM = the fixture JSON (clientId, clientSecret,
// user, password). Skipped without it.
func TestSSOLogin(t *testing.T) {
	requireLive(t)
	raw := os.Getenv("QUILL_E2E_SSO_UPSTREAM")
	if raw == "" {
		t.Skip("QUILL_E2E_SSO_UPSTREAM not set")
	}
	var fx struct{ ClientID, ClientSecret, User, Password string }
	if err := json.Unmarshal([]byte(raw), &fx); err != nil {
		t.Fatalf("fixture: %v", err)
	}
	token := login(t)
	org := fmt.Sprintf("e2e-sso-%d", time.Now().UnixNano()%1e9)
	if st, b := apiCall(t, token, http.MethodPost, "/orgs", map[string]any{"slug": org, "name": "E2E SSO"}); st != 200 && st != 201 {
		t.Fatalf("create org: %d %s", st, b)
	}
	st, b := apiCall(t, token, http.MethodPut, "/orgs/"+org+"/sso", map[string]any{
		"protocol": "oidc", "issuer": authURL, "clientId": fx.ClientID, "clientSecret": fx.ClientSecret,
		"emailDomain": "", "enabled": true,
	})
	if st != 200 {
		t.Fatalf("configure sso: %d %s", st, b)
	}
	cfg := jbody(t, b)
	identityOrg, _ := cfg["identityOrgId"].(string)
	if cfg["active"] != true || identityOrg == "" || cfg["lastError"] != nil {
		t.Fatalf("sso not active: %s", b)
	}
	if cfg["hasSecret"] != true || strings.Contains(string(b), fx.ClientSecret) {
		t.Fatalf("secret handling: %s", b)
	}

	ssoTok := ssoSignIn(t, identityOrg, fx.User, fx.Password)
	st, b = apiCall(t, ssoTok, http.MethodGet, "/auth/me", nil)
	if st != 200 || !strings.Contains(string(b), fx.User) {
		t.Fatalf("sso identity: %d %s", st, b)
	}
	st, b = apiCall(t, ssoTok, http.MethodGet, "/orgs", nil)
	if st != 200 || !strings.Contains(string(b), `"slug":"`+org+`"`) {
		t.Fatalf("SSO sign-in did not join the org: %d %s", st, b)
	}
	// A second sign-in reuses the account.
	again := ssoSignIn(t, identityOrg, fx.User, fx.Password)
	if _, b2 := apiCall(t, again, http.MethodGet, "/auth/me", nil); string(b2) != string(func() []byte { _, x := apiCall(t, ssoTok, http.MethodGet, "/auth/me", nil); return x }()) {
		t.Fatalf("second sign-in resolved a different account: %s", b2)
	}

	// Clean up: the JIT account (its Zitadel org is deleted with the e2e
	// org by purge-e2e, so the next run starts fresh), then the SSO config.
	if st, b := apiCall(t, ssoTok, http.MethodDelete, "/auth/me", nil); st != 200 && st != 204 {
		t.Logf("delete sso account: %d %s", st, b)
	}
	if st, b := apiCall(t, token, http.MethodDelete, "/orgs/"+org+"/sso", nil); st != 204 && st != 200 {
		t.Fatalf("remove sso: %d %s", st, b)
	}
}

var (
	formRe   = regexp.MustCompile(`(?is)<form\b([^>]*)>(.*?)</form>`)
	actionRe = regexp.MustCompile(`(?i)\baction="([^"]*)"`)
	inputRe  = regexp.MustCompile(`(?i)<input\b[^>]*>`)
	attrRe   = regexp.MustCompile(`(?i)\b(name|value|type)="([^"]*)"`)
	titleRe  = regexp.MustCompile(`(?is)<title>(.*?)</title>`)
)

// ssoSignIn runs an org-scoped authorization through whatever Zitadel shows
// (org login → external IdP hop → upstream login name/password → callback →
// possibly an account-confirmation form) by submitting each page's relevant
// form, and returns an access token for the probe client.
func ssoSignIn(t *testing.T, identityOrg, user, pass string) string {
	t.Helper()
	c := client(t)
	verifier := randHex(48)
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])
	redirectURI := appURL + "/api/auth/callback/zitadel"
	scope := "openid profile email urn:zitadel:iam:org:project:id:" + projID + ":aud urn:zitadel:iam:org:id:" + identityOrg
	next := fmt.Sprintf("%s/oauth/v2/authorize?response_type=code&client_id=%s&redirect_uri=%s&scope=%s&state=sso&nonce=sso&code_challenge=%s&code_challenge_method=S256",
		authURL, clientID, url.QueryEscape(redirectURI), url.QueryEscape(scope), challenge)

	var method, body string
	method = http.MethodGet
	trail := []string{}
	for step := 0; step < 30; step++ {
		req, _ := http.NewRequest(method, next, strings.NewReader(body))
		if method == http.MethodPost {
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
		resp, err := c.Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", method, next, err)
		}
		page, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		u, _ := url.Parse(next)
		trail = append(trail, fmt.Sprintf("%s %s -> %d", method, u.Path, resp.StatusCode))
		if resp.StatusCode/100 == 3 {
			loc, _ := u.Parse(resp.Header.Get("Location"))
			if strings.HasPrefix(loc.String(), redirectURI) {
				code := loc.Query().Get("code")
				if code == "" {
					t.Fatalf("callback without code: %s (trail %v)", loc, trail)
				}
				return exchangeCode(t, c, code, verifier, redirectURI)
			}
			method, body, next = http.MethodGet, "", loc.String()
			continue
		}
		if resp.StatusCode != 200 {
			t.Fatalf("unexpected %d at %s (trail %v): %.300s", resp.StatusCode, u.Path, trail, page)
		}
		if d := os.Getenv("QUILL_E2E_SSO_DUMP"); d != "" {
			_ = os.WriteFile(fmt.Sprintf("%s/step-%02d.html", d, step), page, 0o600)
		}
		action, fields, ok := pickForm(string(page), user, pass)
		if !ok {
			title := ""
			if m := titleRe.FindStringSubmatch(string(page)); m != nil {
				title = strings.TrimSpace(m[1])
			}
			t.Fatalf("no usable form at %s (title %q, trail %v)", u.Path, title, trail)
		}
		target, _ := u.Parse(html.UnescapeString(action))
		method, body, next = http.MethodPost, fields.Encode(), target.String()
	}
	t.Fatalf("sign-in did not finish (trail %v)", trail)
	return ""
}

// pickForm chooses the form to submit on a Zitadel login page and fills it.
func pickForm(page, user, pass string) (string, url.Values, bool) {
	forms := formRe.FindAllStringSubmatch(page, -1)
	best, bestScore := -1, -1
	for i, f := range forms {
		score := 0
		switch {
		case strings.Contains(f[2], `name="password"`):
			score = 5
		case strings.Contains(f[2], `name="loginName"`):
			score = 4
		case strings.Contains(f[2], `name="idpID"`) || strings.Contains(strings.ToLower(f[1]), "externalidp"):
			score = 3
		default:
			score = 1
		}
		if score > bestScore {
			best, bestScore = i, score
		}
	}
	if best < 0 {
		return "", nil, false
	}
	f := forms[best]
	am := actionRe.FindStringSubmatch(f[1])
	if am == nil {
		return "", nil, false
	}
	vals := url.Values{}
	for _, in := range inputRe.FindAllString(f[2], -1) {
		var name, value, typ string
		for _, a := range attrRe.FindAllStringSubmatch(in, -1) {
			switch strings.ToLower(a[1]) {
			case "name":
				name = a[2]
			case "value":
				value = html.UnescapeString(a[2])
			case "type":
				typ = strings.ToLower(a[2])
			}
		}
		if name == "" || ((typ == "checkbox" || typ == "radio") && !strings.Contains(strings.ToLower(in), "checked")) {
			continue
		}
		if !vals.Has(name) {
			vals.Set(name, value)
		}
	}
	if vals.Has("loginName") {
		vals.Set("loginName", user)
	}
	if vals.Has("password") {
		vals.Set("password", pass)
	}
	// "External user not found — link or register?" means automatic account
	// creation failed (e.g. a username collision): a real person would be
	// stuck choosing, so the test fails here rather than clicking through.
	if strings.Contains(am[1], "externaluser/option") {
		return "", nil, false
	}
	if strings.Contains(am[1], "mfa/prompt") {
		vals.Set("skip", "true")
	}
	// IdP selection buttons carry the idp id on the button, not an input.
	if !vals.Has("idpID") {
		if m := regexp.MustCompile(`(?i)name="idpID"[^>]*value="([^"]+)"`).FindStringSubmatch(f[2]); m != nil {
			vals.Set("idpID", m[1])
		}
	}
	return am[1], vals, true
}

func exchangeCode(t *testing.T, c *http.Client, code, verifier, redirectURI string) string {
	t.Helper()
	form := url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {redirectURI}, "client_id": {clientID}, "code_verifier": {verifier}}
	req, _ := http.NewRequest(http.MethodPost, authURL+"/oauth/v2/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if s := os.Getenv("QUILL_E2E_CLIENT_SECRET"); s != "" {
		req.SetBasicAuth(clientID, s)
	}
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("token exchange: %v", err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	var tok struct {
		AccessToken string `json:"access_token"`
	}
	if resp.StatusCode != 200 || json.Unmarshal(b, &tok) != nil || tok.AccessToken == "" {
		t.Fatalf("token exchange: %d %s", resp.StatusCode, b)
	}
	return tok.AccessToken
}
