// Package live contains end-to-end tests that run against the DEPLOYED
// platform (Scaleway), not a local stack. They are skipped unless
// QUILL_LIVE_TEST=1 is set, so `./check` / CI never execute them.
//
// Required env:
//
//	QUILL_LIVE_TEST=1
//	QUILL_E2E_API_URL      e.g. https://api.black-tulip.nl   (default)
//	QUILL_E2E_APP_URL      e.g. https://app.black-tulip.nl   (default)
//	QUILL_E2E_AUTH_URL     e.g. https://auth.black-tulip.nl  (default)
//	QUILL_E2E_CLIENT_ID    Zitadel OIDC client id of the web app
//	QUILL_E2E_PROJECT_ID   Zitadel project id (for the :aud scope)
//	QUILL_E2E_USER         test user login name        (default e2e-tester)
//	QUILL_E2E_PASSWORD     test user password
package live

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"
	"time"
)

var (
	apiURL   = env("QUILL_E2E_API_URL", "https://api.black-tulip.nl")
	appURL   = env("QUILL_E2E_APP_URL", "https://app.black-tulip.nl")
	authURL  = env("QUILL_E2E_AUTH_URL", "https://auth.black-tulip.nl")
	clientID = os.Getenv("QUILL_E2E_CLIENT_ID")
	projID   = os.Getenv("QUILL_E2E_PROJECT_ID")
	e2eUser  = env("QUILL_E2E_USER", "e2e-tester")
	e2ePass  = os.Getenv("QUILL_E2E_PASSWORD")
)

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func requireLive(t *testing.T) {
	t.Helper()
	if os.Getenv("QUILL_LIVE_TEST") != "1" {
		t.Skip("QUILL_LIVE_TEST not set; skipping live e2e tests")
	}
	if clientID == "" || projID == "" || e2ePass == "" {
		t.Skip("QUILL_E2E_CLIENT_ID / QUILL_E2E_PROJECT_ID / QUILL_E2E_PASSWORD required")
	}
}

// client returns an HTTP client with a cookie jar (needed for the Zitadel
// login form flow).
func client(t *testing.T) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar: %v", err)
	}
	return &http.Client{
		Jar:     jar,
		Timeout: 30 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse // we inspect redirects manually
		},
	}
}

func get(t *testing.T, c *http.Client, rawurl string, headers map[string]string) (int, http.Header, []byte) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, rawurl, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", rawurl, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return resp.StatusCode, resp.Header, body
}

func req(t *testing.T, c *http.Client, method, rawurl string, body any, headers map[string]string) (int, http.Header, []byte) {
	t.Helper()
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, rawurl, rdr)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, rawurl, err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return resp.StatusCode, resp.Header, b
}

// login performs the OIDC authorization-code + PKCE flow headlessly against
// Zitadel (login form v1: the form posts gorilla.csrf.Token, authRequestID,
// loginName, password) and returns a Quill-usable access token.
func login(t *testing.T) string {
	t.Helper()
	return loginAs(t, e2eUser, e2ePass)
}

func loginAs(t *testing.T, user, pass string) string {
	t.Helper()
	c := client(t)

	verifier := randHex(48)
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])
	redirectURI := appURL + "/api/auth/callback/zitadel"
	scope := "openid profile email offline_access urn:zitadel:iam:org:project:id:" + projID + ":aud"

	authReq := fmt.Sprintf("%s/oauth/v2/authorize?response_type=code&client_id=%s&redirect_uri=%s&scope=%s&state=e2e&nonce=e2e&code_challenge=%s&code_challenge_method=S256",
		authURL, clientID, url.QueryEscape(redirectURI), url.QueryEscape(scope), challenge)

	st, hdr, page := get(t, c, authReq, nil)
	// Zitadel serves the login page two ways depending on version/flow state:
	// a 302 to /ui/login/login?authRequestID=..., or the login form rendered
	// inline with 200. Both carry the gorilla.csrf.Token + authRequestID the
	// stepwise login posts, so accept either.
	switch st {
	case http.StatusFound:
		loginPage := hdr.Get("Location")
		if !strings.HasPrefix(loginPage, "/") {
			t.Fatalf("authorize redirect not relative: %s", loginPage)
		}
		st, _, page = get(t, c, authURL+loginPage, nil)
		if st != http.StatusOK {
			t.Fatalf("login page: got %d", st)
		}
	case http.StatusOK:
		// login form is already in `page`
	default:
		t.Fatalf("authorize: got %d, want 200 or 302", st)
	}

	// Zitadel login UI is stepwise: loginname -> password -> mfa/skip ->
	// authorize/callback. Each step posts the form's gorilla.csrf.Token and
	// authRequestID back. The login name must be the user's email.
	formPost := func(path string, fields url.Values) (int, http.Header, []byte) {
		preq, _ := http.NewRequest(http.MethodPost, authURL+path, strings.NewReader(fields.Encode()))
		preq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		resp, err := c.Do(preq)
		if err != nil {
			t.Fatalf("POST %s: %v", path, err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, resp.Header, b
	}

	csrf := extractInput(page, "gorilla.csrf.Token")
	authID := extractInput(page, "authRequestID")
	if csrf == "" || authID == "" {
		t.Fatalf("login form fields not found (csrf=%q authID=%q)", csrf, authID)
	}
	base := url.Values{"gorilla.csrf.Token": {csrf}, "authRequestID": {authID}}

	// step 1: loginname (the CSRF token rotates per page — re-read each step)
	st, hdr, page = formPost("/ui/login/loginname", with(base, "loginName", user))
	if cs := extractInput(page, "gorilla.csrf.Token"); cs != "" {
		base.Set("gorilla.csrf.Token", cs)
	}
	// step 2: password (may 200 with an MFA page, or 302 straight to callback)
	st, hdr, page = formPost("/ui/login/password", with(base, "password", pass))
	if cs := extractInput(page, "gorilla.csrf.Token"); cs != "" {
		base.Set("gorilla.csrf.Token", cs)
	}
	// step 3: skip MFA prompt if present
	if st != http.StatusFound && strings.Contains(string(page), "/ui/login/mfa/prompt") {
		st, hdr, _ = formPost("/ui/login/mfa/prompt", with(base, "skip", "true"))
		if st != http.StatusFound {
			t.Fatalf("mfa skip: got %d", st)
		}
	}
	if st == http.StatusFound && strings.Contains(hdr.Get("Location"), "/oauth/v2/authorize/callback") {
		loc := hdr.Get("Location")
		if strings.HasPrefix(loc, "http") {
			st, hdr, _ = get(t, c, loc, nil)
		} else {
			st, hdr, _ = get(t, c, authURL+loc, nil)
		}
		if st != http.StatusFound {
			t.Fatalf("authorize callback: got %d", st)
		}
	}
	cb := hdr.Get("Location")
	code := extractQuery(cb, "code")
	if code == "" {
		// Surface Zitadel's own error text (wrong password, locked user,
		// unknown login name, init/password-change required, ...).
		msg := ""
		for _, cls := range []string{"lgn-error-message", "error-msg", "lgn-error"} {
			if i := strings.Index(string(page), cls); i >= 0 {
				end := i + 400
				if end > len(page) {
					end = len(page)
				}
				msg = string(page[i:end])
				break
			}
		}
		title := ""
		if i := strings.Index(string(page), "<title>"); i >= 0 {
			if j := strings.Index(string(page[i:]), "</title>"); j > 0 {
				title = string(page[i+7 : i+j])
			}
		}
		t.Fatalf("no code in callback redirect: %q (last status %d, page title %q, error %q)", cb, st, title, msg)
	}

	tokForm := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {redirectURI},
		"client_id":     {clientID},
		"code_verifier": {verifier},
	}
	// Confidential OIDC app: authenticate the token exchange with the client
	// secret (Basic auth). Uses the app's env-var pattern: skipped when unset
	// (public PKCE client, e.g. local development against a public app).
	clientSecret := os.Getenv("QUILL_E2E_CLIENT_SECRET")
	treq, _ := http.NewRequest(http.MethodPost, authURL+"/oauth/v2/token", strings.NewReader(tokForm.Encode()))
	treq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if clientSecret != "" {
		treq.SetBasicAuth(clientID, clientSecret)
	}
	tresp, err := c.Do(treq)
	if err != nil {
		t.Fatalf("token exchange: %v", err)
	}
	defer tresp.Body.Close()
	tb, _ := io.ReadAll(tresp.Body)
	if tresp.StatusCode != http.StatusOK {
		t.Fatalf("token exchange: %d %s", tresp.StatusCode, tb)
	}
	var tok struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(tb, &tok); err != nil || tok.AccessToken == "" {
		t.Fatalf("parse token response: %v %s", err, tb)
	}
	return tok.AccessToken
}

// prNumber extracts the PR number from a create/get response, tolerating the
// (repo_slug, quill_pr_number) identity contract field names.
func prNumber(t *testing.T, m map[string]any) int {
	t.Helper()
	if inner, ok := m["pull"].(map[string]any); ok {
		m = inner
	}
	for _, k := range []string{"quill_pr_number", "number", "pr_number"} {
		switch v := m[k].(type) {
		case float64:
			if v > 0 {
				return int(v)
			}
		case string:
			var n int
			if _, err := fmt.Sscanf(v, "%d", &n); err == nil && n > 0 {
				return n
			}
		}
	}
	t.Fatalf("no PR number in response: %v", m)
	return 0
}

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func with(base url.Values, k, v string) url.Values {
	out := url.Values{}
	for key, vals := range base {
		out[key] = vals
	}
	out.Set(k, v)
	return out
}

func extractInput(page []byte, name string) string {
	s := string(page)
	needle := `name="` + name + `"`
	i := strings.Index(s, needle)
	if i < 0 {
		return ""
	}
	rest := s[i:]
	j := strings.Index(rest, `value="`)
	if j < 0 {
		return ""
	}
	rest = rest[j+len(`value="`):]
	k := strings.Index(rest, `"`)
	if k < 0 {
		return ""
	}
	return rest[:k]
}

func extractQuery(rawurl, key string) string {
	u, err := url.Parse(rawurl)
	if err != nil {
		return ""
	}
	return u.Query().Get(key)
}

// apiCall hits the Quill API with the bearer token.
func apiCall(t *testing.T, token, method, path string, body any) (int, []byte) {
	t.Helper()
	c := client(t)
	st, _, b := req(t, c, method, apiURL+"/api/v1"+path, body, map[string]string{
		"Authorization": "Bearer " + token,
	})
	return st, b
}

func jbody(t *testing.T, b []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("response not JSON: %s", b)
	}
	return m
}

// ---- tests -------------------------------------------------------------------

func TestHealthAndMeta(t *testing.T) {
	requireLive(t)
	c := client(t)
	if st, _, _ := get(t, c, apiURL+"/healthz", nil); st != 200 {
		t.Fatalf("healthz: %d", st)
	}
	st, _, b := get(t, c, apiURL+"/api/v1/meta", nil)
	if st != 200 {
		t.Fatalf("meta: %d %s", st, b)
	}
}

func TestWebPages(t *testing.T) {
	requireLive(t)
	c := client(t)
	// "/" redirects to /sign-in when logged out; /sign-in must be 200
	if st, hdr, _ := get(t, c, appURL+"/", nil); st != 307 || !strings.Contains(hdr.Get("Location"), "sign-in") {
		t.Errorf("GET /: %d -> %s, want 307 to /sign-in", st, hdr.Get("Location"))
	}
	if st, _, _ := get(t, c, appURL+"/sign-in", nil); st != 200 {
		t.Errorf("GET /sign-in: %d, want 200", st)
	}
	// /login is an alias for /sign-in
	if st, hdr, _ := get(t, c, appURL+"/login", nil); st != 307 || !strings.Contains(hdr.Get("Location"), "/sign-in") {
		t.Errorf("GET /login: %d -> %s, want 307 to /sign-in", st, hdr.Get("Location"))
	}
	// unauthenticated app pages must redirect to sign-in, not 500
	for _, p := range []string{"/repositories", "/onboarding", "/settings"} {
		st, hdr, _ := get(t, c, appURL+p, nil)
		if st >= 500 {
			t.Errorf("GET %s: server error %d", p, st)
		}
		if st == 200 || st == 307 {
			if loc := hdr.Get("Location"); st == 307 && !strings.Contains(loc, "sign-in") {
				t.Errorf("GET %s: 307 to unexpected %s", p, loc)
			}
		}
	}
}

func TestAuthMeAndUsernameRoundtrip(t *testing.T) {
	requireLive(t)
	token := login(t)

	st, b := apiCall(t, token, http.MethodGet, "/auth/me", nil)
	if st != 200 {
		t.Fatalf("auth/me: %d %s", st, b)
	}
	me := jbody(t, b)
	wantUser := strings.Split(e2eUser, "@")[0] // Zitadel derives the handle from the email local part
	if me["username"] != wantUser {
		t.Fatalf("auth/me username = %v, want %s", me["username"], wantUser)
	}

	// The original onboarding bug: renaming the handle must work (it 500'd
	// when the Forgejo rename was sent as JSON).
	st, b = apiCall(t, token, http.MethodPatch, "/auth/me/username", map[string]any{"username": wantUser + "-2"})
	if st != 200 {
		t.Fatalf("PATCH username: %d %s", st, b)
	}
	if u := jbody(t, b)["username"]; u != wantUser+"-2" {
		t.Fatalf("username after PATCH = %v", u)
	}
	// and back
	if st, b = apiCall(t, token, http.MethodPatch, "/auth/me/username", map[string]any{"username": wantUser}); st != 200 {
		t.Fatalf("PATCH username back: %d %s", st, b)
	}
}

func TestPersonalProjectAndRepos(t *testing.T) {
	requireLive(t)
	token := login(t)

	st, b := apiCall(t, token, http.MethodPost, "/me/personal-project", nil)
	if st != 200 && st != 409 {
		t.Fatalf("personal-project: %d %s", st, b)
	}

	st, b = apiCall(t, token, http.MethodGet, "/me/projects", nil)
	if st != 200 {
		t.Fatalf("me/projects: %d %s", st, b)
	}
	projects := jbody(t, b)["projects"].([]any)
	if len(projects) == 0 {
		t.Fatalf("no projects after personal-project: %s", b)
	}
	slug, _ := projects[0].(map[string]any)["slug"].(string)

	// create a repo
	repoSlug := fmt.Sprintf("e2e-repo-%d", time.Now().UnixNano()%1e9)
	st, b = apiCall(t, token, http.MethodPost, "/projects/"+slug+"/repos", map[string]any{
		"slug": repoSlug, "name": repoSlug, "description": "e2e", "visibility": "private",
	})
	if st != 200 && st != 201 {
		t.Fatalf("create repo: %d %s", st, b)
	}

	// list + get
	st, b = apiCall(t, token, http.MethodGet, "/projects/"+slug+"/repos", nil)
	if st != 200 {
		t.Fatalf("list repos: %d %s", st, b)
	}
	st, b = apiCall(t, token, http.MethodGet, "/projects/"+slug+"/repos/"+repoSlug, nil)
	if st != 200 {
		t.Fatalf("get repo: %d %s", st, b)
	}
	// branches/commits/contents on an empty repo (ErrEmptyRepo is a 409)
	st, _ = apiCall(t, token, http.MethodGet, "/projects/"+slug+"/repos/"+repoSlug+"/branches", nil)
	if st >= 500 {
		t.Errorf("branches on empty repo: %d (want 200/404/409)", st)
	}
	st, _ = apiCall(t, token, http.MethodGet, "/projects/"+slug+"/repos/"+repoSlug+"/commits", nil)
	if st >= 500 {
		t.Errorf("commits on empty repo: %d", st)
	}
}

func TestPullsPipelinesIssues(t *testing.T) {
	requireLive(t)
	token := login(t)

	st, b := apiCall(t, token, http.MethodGet, "/me/projects", nil)
	if st != 200 {
		t.Fatalf("me/projects: %d %s", st, b)
	}
	projects := jbody(t, b)["projects"].([]any)
	slug, _ := projects[0].(map[string]any)["slug"].(string)

	for _, p := range []string{
		"/me/pulls", "/me/pulls/open-count",
		"/projects/" + slug + "/repos",
	} {
		if st, b := apiCall(t, token, http.MethodGet, p, nil); st >= 500 {
			t.Errorf("GET %s: %d %s", p, st, b)
		}
	}

	// create issue + comment + edit on a repo
	st, b = apiCall(t, token, http.MethodPost, "/me/personal-project", nil)
	if st != 200 && st != 409 {
		t.Fatalf("personal-project: %d %s", st, b)
	}
	repoSlug := fmt.Sprintf("e2e-issues-%d", time.Now().UnixNano()%1e9)
	if st, b = apiCall(t, token, http.MethodPost, "/projects/"+slug+"/repos", map[string]any{"slug": repoSlug, "name": repoSlug}); st != 200 && st != 201 {
		t.Fatalf("create repo: %d %s", st, b)
	}
	st, b = apiCall(t, token, http.MethodPost, "/projects/"+slug+"/repos/"+repoSlug+"/issues", map[string]any{"title": "e2e issue", "body": "hello"})
	if st != 200 && st != 201 {
		t.Fatalf("create issue: %d %s", st, b)
	}
	st, b = apiCall(t, token, http.MethodGet, "/projects/"+slug+"/repos/"+repoSlug+"/issues", nil)
	if st != 200 {
		t.Errorf("list issues: %d %s", st, b)
	}
}

func TestGitTokensAndSSHKeys(t *testing.T) {
	requireLive(t)
	token := login(t)

	st, b := apiCall(t, token, http.MethodPost, "/me/git-token", map[string]any{"name": "e2e"})
	if st != 200 && st != 201 {
		t.Fatalf("create git token: %d %s", st, b)
	}
	st, b = apiCall(t, token, http.MethodGet, "/me/git-tokens", nil)
	if st != 200 {
		t.Errorf("list git tokens: %d %s", st, b)
	}

	st, b = apiCall(t, token, http.MethodPost, "/me/ssh-keys", map[string]any{"name": "e2e", "key": "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIE2eTestKeyDoNotUse000000000000000000000000000 e2e"})
	if st >= 500 {
		t.Errorf("add ssh key: %d %s", st, b)
	}
	st, b = apiCall(t, token, http.MethodGet, "/me/ssh-keys", nil)
	if st != 200 {
		t.Errorf("list ssh keys: %d %s", st, b)
	}
}

func TestAdminForbiddenForUser(t *testing.T) {
	requireLive(t)
	token := login(t)
	st, b := apiCall(t, token, http.MethodGet, "/admin/users", nil)
	if st != 403 && st != 401 {
		t.Errorf("admin/users as regular user: %d %s, want 403", st, b)
	}
}

func TestGitHubImportEndpoints(t *testing.T) {
	requireLive(t)
	token := login(t)
	// Without the OAuth cookie, listing repos must be a clean 401 (not 500).
	st, b := apiCall(t, token, http.MethodGet, "/import/github/repos", nil)
	if st != 401 {
		t.Errorf("import/github/repos without GH cookie: %d %s, want 401", st, b)
	}
	// The OAuth start must either redirect to github.com (creds configured)
	// or return a clean error (not a raw crash). Accept 302-to-github or 501.
	c := client(t)
	st, hdr, _ := get(t, c, apiURL+"/api/v1/auth/github", nil)
	if st == 302 && strings.HasPrefix(hdr.Get("Location"), "https://github.com/login/oauth/authorize") {
		return // configured and redirecting — good
	}
	if st != 501 && st != 302 {
		t.Errorf("auth/github: %d (want 302 to github or 501 not_configured)", st)
	}
}

// ---- extended flows ----------------------------------------------------------

func sh(t *testing.T, dir, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	// Never block on a credential prompt: a rejected token must fail fast.
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		// Clone URLs embed the git token: never print it.
		t.Fatalf("%s %v: %v\n%s", name, redactTokens(fmt.Sprint(args)), err, redactTokens(string(out)))
	}
	return string(out)
}

var tokenRE = regexp.MustCompile(`q(gt|gr)_[A-Za-z0-9._-]+`)

func redactTokens(s string) string { return tokenRE.ReplaceAllString(s, "q${1}_***") }

// setupProjectRepo logs in, ensures a personal project, creates a repo and
// returns (token, projectSlug, repoSlug).
func setupProjectRepo(t *testing.T, suffix string) (string, string, string) {
	token := login(t)
	if st, b := apiCall(t, token, http.MethodPost, "/me/personal-project", nil); st != 200 && st != 409 {
		t.Fatalf("personal-project: %d %s", st, b)
	}
	st, b := apiCall(t, token, http.MethodGet, "/me/projects", nil)
	if st != 200 {
		t.Fatalf("me/projects: %d %s", st, b)
	}
	projects := jbody(t, b)["projects"].([]any)
	slug, _ := projects[0].(map[string]any)["slug"].(string)
	repoSlug := fmt.Sprintf("e2e-%s-%d", suffix, time.Now().UnixNano()%1e9)
	if st, b := apiCall(t, token, http.MethodPost, "/projects/"+slug+"/repos", map[string]any{
		"slug": repoSlug, "name": repoSlug, "visibility": "private",
	}); st != 200 && st != 201 {
		t.Fatalf("create repo: %d %s", st, b)
	}
	return token, slug, repoSlug
}

func TestGitRoundtripAndPullRequest(t *testing.T) {
	requireLive(t)
	token, proj, repo := setupProjectRepo(t, "git")

	// git token
	st, b := apiCall(t, token, http.MethodPost, "/me/git-token", map[string]any{"name": "e2e"})
	if st != 200 && st != 201 {
		t.Fatalf("git token: %d %s", st, b)
	}
	cred := jbody(t, b)
	tok, _ := cred["token"].(string)
	if tok == "" {
		t.Fatalf("git token response missing token: %s", b)
	}
	_, meB := apiCall(t, token, http.MethodGet, "/auth/me", nil)
	username, _ := jbody(t, meB)["username"].(string)

	// clone + commit + push over HTTPS
	dir := t.TempDir()
	cloneURL := gitCloneURL(t, username, tok, proj, repo)
	sh(t, dir, "git", "clone", cloneURL, "repo")
	rdir := dir + "/repo"
	sh(t, rdir, "git", "config", "user.email", "e2e@black-tulip.nl")
	sh(t, rdir, "git", "config", "user.name", "e2e")
	if err := os.WriteFile(rdir+"/README.md", []byte("# e2e\nhello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sh(t, rdir, "git", "add", "README.md")
	sh(t, rdir, "git", "commit", "-m", "e2e initial commit")
	sh(t, rdir, "git", "branch", "-M", "main")
	sh(t, rdir, "git", "push", "-u", "origin", "main")
	// a feature branch for the PR
	sh(t, rdir, "git", "checkout", "-b", "feature/e2e")
	if err := os.WriteFile(rdir+"/feature.txt", []byte("feature\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sh(t, rdir, "git", "add", "feature.txt")
	sh(t, rdir, "git", "commit", "-m", "add feature")
	sh(t, rdir, "git", "push", "-u", "origin", "feature/e2e")

	// API sees the branches/commits/contents
	for _, p := range []string{"/branches", "/commits", "/contents?ref=main"} {
		if st, b := apiCall(t, token, http.MethodGet, "/projects/"+proj+"/repos/"+repo+p, nil); st >= 500 {
			t.Errorf("GET %s: %d %s", p, st, b)
		}
	}

	// open a PR
	st, b = apiCall(t, token, http.MethodPost, "/projects/"+proj+"/repos/"+repo+"/pulls", map[string]any{
		"title": "e2e PR", "body": "from e2e", "base": "main", "head": "feature/e2e",
	})
	if st != 200 && st != 201 {
		t.Fatalf("create PR: %d %s", st, b)
	}
	pr := jbody(t, b)
	num := prNumber(t, pr)
	st, b = apiCall(t, token, http.MethodGet, "/projects/"+proj+"/repos/"+repo+"/pulls", nil)
	if st != 200 {
		t.Errorf("list PRs: %d %s", st, b)
	}
	st, b = apiCall(t, token, http.MethodGet, fmt.Sprintf("/projects/%s/repos/%s/pulls/%d", proj, repo, int(num)), nil)
	if st != 200 {
		t.Errorf("get PR: %d %s", st, b)
	}
	st, b = apiCall(t, token, http.MethodGet, fmt.Sprintf("/projects/%s/repos/%s/pulls/%d/diff", proj, repo, int(num)), nil)
	if st >= 500 {
		t.Errorf("PR diff: %d %s", st, b)
	}
	st, b = apiCall(t, token, http.MethodPost, fmt.Sprintf("/projects/%s/repos/%s/pulls/%d/comments", proj, repo, int(num)), map[string]any{"body": "e2e comment"})
	if st != 200 && st != 201 {
		t.Errorf("PR comment: %d %s", st, b)
	}
}

func TestPipelines(t *testing.T) {
	requireLive(t)
	token, proj, repo := setupProjectRepo(t, "pipe")

	base := "/projects/" + proj + "/repos/" + repo + "/pipelines"
	if st, b := apiCall(t, token, http.MethodGet, base, nil); st >= 500 {
		t.Errorf("list pipelines: %d %s", st, b)
	}
	if st, b := apiCall(t, token, http.MethodGet, base+"/runs", nil); st >= 500 {
		t.Errorf("list runs: %d %s", st, b)
	}
	// trigger a run
	st, b := apiCall(t, token, http.MethodPost, base, map[string]any{
		"ref": "main", "steps": []string{"echo hello", "echo done"},
	})
	if st >= 500 {
		t.Errorf("trigger run: %d %s", st, b)
	}
}

func TestOrgsGroupsBillingPoliciesExport(t *testing.T) {
	requireLive(t)
	token := login(t)

	orgSlug := fmt.Sprintf("e2e-org-%d", time.Now().UnixNano()%1e9)
	st, b := apiCall(t, token, http.MethodPost, "/orgs", map[string]any{"slug": orgSlug, "name": orgSlug})
	if st != 200 && st != 201 && st != 409 {
		t.Errorf("create org: %d %s", st, b)
	}
	if st, b := apiCall(t, token, http.MethodGet, "/orgs", nil); st >= 500 {
		t.Errorf("list orgs: %d %s", st, b)
	}
	if st, b := apiCall(t, token, http.MethodGet, "/orgs/"+orgSlug+"/billing", nil); st >= 500 {
		t.Errorf("org billing: %d %s", st, b)
	}
	// group create/list inside the org
	if st, b := apiCall(t, token, http.MethodPost, "/orgs/"+orgSlug+"/groups", map[string]any{"name": "e2e-team"}); st >= 500 {
		t.Errorf("create group: %d %s", st, b)
	}
	if st, b := apiCall(t, token, http.MethodGet, "/orgs/"+orgSlug+"/groups", nil); st >= 500 {
		t.Errorf("list groups: %d %s", st, b)
	}
	// project policy set/get/delete on the personal project
	st, b = apiCall(t, token, http.MethodGet, "/me/projects", nil)
	projects := jbody(t, b)["projects"].([]any)
	pslug, _ := projects[0].(map[string]any)["slug"].(string)
	pol := "/projects/" + pslug + "/policies"
	if st, b := apiCall(t, token, http.MethodPut, pol, map[string]any{"requireApprovals": 1}); st >= 500 {
		t.Errorf("set policy: %d %s", st, b)
	}
	if st, b := apiCall(t, token, http.MethodGet, pol, nil); st >= 500 {
		t.Errorf("get policies: %d %s", st, b)
	}
	if st, b := apiCall(t, token, http.MethodDelete, pol, nil); st >= 500 && st != 404 {
		t.Errorf("delete policy: %d %s", st, b)
	}
	// data export
	if st, b := apiCall(t, token, http.MethodGet, "/me/export", nil); st >= 500 {
		t.Errorf("export: %d %s", st, b)
	}
	// contributions
	if st, b := apiCall(t, token, http.MethodGet, "/me/contributions", nil); st >= 500 {
		t.Errorf("contributions: %d %s", st, b)
	}
}

// ---- round 2: PR review/merge, repo ops, secrets, invites -------------------

func TestPRReviewAndMerge(t *testing.T) {
	requireLive(t)
	token, proj, repo := setupProjectRepo(t, "merge")

	// push main + feature branch
	st, b := apiCall(t, token, http.MethodPost, "/me/git-token", map[string]any{"name": "e2e"})
	if st != 200 && st != 201 {
		t.Fatalf("git token: %d %s", st, b)
	}
	cred := jbody(t, b)
	tok, _ := cred["token"].(string)
	username, _ := cred["username"].(string)
	if tok == "" {
		t.Fatalf("git token empty: %s", b)
	}
	dir := t.TempDir()
	sh(t, dir, "git", "clone", gitCloneURL(t, username, tok, proj, repo), "r")
	rd := dir + "/r"
	sh(t, rd, "git", "config", "user.email", "e2e@black-tulip.nl")
	sh(t, rd, "git", "config", "user.name", "e2e")
	_ = os.WriteFile(rd+"/a.txt", []byte("a\n"), 0o644)
	sh(t, rd, "git", "add", ".")
	sh(t, rd, "git", "commit", "-m", "c1")
	sh(t, rd, "git", "branch", "-M", "main")
	sh(t, rd, "git", "push", "-u", "origin", "main")
	sh(t, rd, "git", "checkout", "-b", "feat")
	_ = os.WriteFile(rd+"/b.txt", []byte("b\n"), 0o644)
	sh(t, rd, "git", "add", ".")
	sh(t, rd, "git", "commit", "-m", "c2")
	sh(t, rd, "git", "push", "-u", "origin", "feat")

	rp := "/projects/" + proj + "/repos/" + repo
	st, b = apiCall(t, token, http.MethodPost, rp+"/pulls", map[string]any{"title": "merge me", "base": "main", "head": "feat"})
	if st != 200 && st != 201 {
		t.Fatalf("create PR: %d %s", st, b)
	}
	num := prNumber(t, jbody(t, b))

	// review (approve) + line comment + merge
	if st, b := apiCall(t, token, http.MethodPost, fmt.Sprintf("%s/pulls/%d/reviews", rp, num), map[string]any{"event": "APPROVED", "body": "lgtm"}); st >= 500 {
		t.Errorf("review: %d %s", st, b)
	}
	if st, b := apiCall(t, token, http.MethodPost, fmt.Sprintf("%s/pulls/%d/line-comments", rp, num), map[string]any{"body": "nit", "path": "b.txt", "line": 1}); st >= 500 {
		t.Errorf("line comment: %d %s", st, b)
	}
	if st, b := apiCall(t, token, http.MethodGet, fmt.Sprintf("%s/pulls/%d/reviews", rp, num), nil); st >= 500 {
		t.Errorf("list reviews: %d %s", st, b)
	}
	if st, b := apiCall(t, token, http.MethodPost, fmt.Sprintf("%s/pulls/%d/merge", rp, num), map[string]any{}); st >= 500 {
		t.Errorf("merge: %d %s", st, b)
	}
	st, b = apiCall(t, token, http.MethodGet, fmt.Sprintf("%s/pulls/%d", rp, num), nil)
	if st == 200 {
		state, _ := jbody(t, b)["pull"].(map[string]any)["state"].(string)
		t.Logf("PR state after merge attempt: %s", state)
	}
}

func TestRepoOpsSecretsInvites(t *testing.T) {
	requireLive(t)
	token, proj, repo := setupProjectRepo(t, "ops")
	rp := "/projects/" + proj + "/repos/" + repo

	// update repo, star, fork
	if st, b := apiCall(t, token, http.MethodPatch, rp, map[string]any{"description": "updated by e2e"}); st >= 500 {
		t.Errorf("PATCH repo: %d %s", st, b)
	}
	if st, b := apiCall(t, token, http.MethodPost, rp+"/star", nil); st >= 500 {
		t.Errorf("star: %d %s", st, b)
	}
	if st, b := apiCall(t, token, http.MethodPost, rp+"/fork", nil); st >= 500 {
		t.Errorf("fork: %d %s", st, b)
	}
	// markup render
	if st, b := apiCall(t, token, http.MethodPost, rp+"/markup", map[string]any{"text": "# hi", "mode": "markdown"}); st >= 500 {
		t.Errorf("markup: %d %s", st, b)
	}
	// repo secrets CRUD
	sec := rp + "/secrets"
	if st, b := apiCall(t, token, http.MethodPut, sec+"/E2E_VAR", map[string]any{"value": "s3cr3t"}); st >= 500 && st != 404 {
		t.Errorf("put repo secret: %d %s", st, b)
	}
	if st, b := apiCall(t, token, http.MethodGet, sec, nil); st >= 500 {
		t.Errorf("list repo secrets: %d %s", st, b)
	}
	if st, b := apiCall(t, token, http.MethodDelete, sec+"/E2E_VAR", nil); st >= 500 && st != 404 {
		t.Errorf("delete repo secret: %d %s", st, b)
	}
	// invite create/list/revoke on the org
	orgSlug := fmt.Sprintf("e2e-inv-%d", time.Now().UnixNano()%1e9)
	if st, b := apiCall(t, token, http.MethodPost, "/orgs", map[string]any{"slug": orgSlug, "name": orgSlug}); st != 200 && st != 201 && st != 409 {
		t.Fatalf("create org: %d %s", st, b)
	}
	st, b := apiCall(t, token, http.MethodPost, "/orgs/"+orgSlug+"/invites", map[string]any{"email": "invitee@black-tulip.nl", "role": "member"})
	if st != 200 && st != 201 {
		t.Errorf("create invite: %d %s", st, b)
	}
	st, b = apiCall(t, token, http.MethodGet, "/orgs/"+orgSlug+"/invites", nil)
	if st != 200 {
		t.Errorf("list invites: %d %s", st, b)
	} else {
		inv := jbody(t, b)
		if arr, ok := inv["invites"].([]any); ok && len(arr) > 0 {
			id, _ := arr[0].(map[string]any)["id"].(string)
			if st, b := apiCall(t, token, http.MethodDelete, "/orgs/"+orgSlug+"/invites/"+id, nil); st >= 500 {
				t.Errorf("revoke invite: %d %s", st, b)
			}
		}
	}
	// delete the repo again (cleanup)
	if st, b := apiCall(t, token, http.MethodDelete, rp, nil); st >= 500 {
		t.Errorf("delete repo: %d %s", st, b)
	}
}
