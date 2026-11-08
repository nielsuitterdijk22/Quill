package live

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestProbeOrgsAndRepos(t *testing.T) {
	requireLive(t)
	token := login(t)
	st, b := apiCall(t, token, http.MethodGet, "/orgs", nil)
	fmt.Printf("ORGS %d: %.400s\n", st, b)
	st, b = apiCall(t, token, http.MethodGet, "/me/projects", nil)
	fmt.Printf("PROJECTS %d: %.400s\n", st, b)
	projs, _ := jbody(t, b)["projects"].([]any)
	{
		for _, p := range projs {
			slug, _ := p.(map[string]any)["slug"].(string)
			st2, b2 := apiCall(t, token, http.MethodGet, "/projects/"+slug+"/repos", nil)
			fmt.Printf("REPOS %s %d: %.200s\n", slug, st2, b2)
		}
	}
}

// TestWebRepositoriesPage drives the real app signin flow (app -> Zitadel ->
// app callback) and then fetches /repositories with the session cookie.
func TestWebRepositoriesPage(t *testing.T) {
	requireLive(t)
	c := client(t)

	// 1. app csrf + signin POST -> authorize URL (sets state cookie)
	_, _, b := get(t, c, appURL+"/api/auth/csrf", nil)
	csrf := jbody(t, b)["csrfToken"].(string)
	st, hdr, _ := req(t, c, http.MethodPost, appURL+"/api/auth/signin/zitadel", nil, map[string]string{"Content-Type": "application/x-www-form-urlencoded"})
	_ = st
	preq, _ := http.NewRequest(http.MethodPost, appURL+"/api/auth/signin/zitadel", strings.NewReader(url.Values{"csrfToken": {csrf}, "callbackUrl": {appURL + "/"}}.Encode()))
	preq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.Do(preq)
	if err != nil || resp.StatusCode != 302 {
		t.Fatalf("app signin POST: %v %d", err, resp.StatusCode)
	}
	authReq := resp.Header.Get("Location")
	resp.Body.Close()

	// 2. follow to Zitadel login and authenticate (same helper flow as login())
	st, hdr, _ = get(t, c, authReq, nil)
	if st != 302 {
		t.Fatalf("authorize: %d", st)
	}
	loginPage := hdr.Get("Location")
	if !strings.HasPrefix(loginPage, "/") {
		loginPage = "/" + strings.TrimPrefix(loginPage, authURL)
	}
	_, _, page := get(t, c, authURL+loginPage, nil)
	base := url.Values{
		"gorilla.csrf.Token": {extractInput(page, "gorilla.csrf.Token")},
		"authRequestID":      {extractInput(page, "authRequestID")},
	}
	formPost := func(path string, fields url.Values) (int, http.Header, []byte) {
		preq, _ := http.NewRequest(http.MethodPost, authURL+path, strings.NewReader(fields.Encode()))
		preq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r, err := c.Do(preq)
		if err != nil {
			t.Fatalf("POST %s: %v", path, err)
		}
		defer r.Body.Close()
		b, _ := io.ReadAll(r.Body)
		return r.StatusCode, r.Header, b
	}
	_, _, page = formPost("/ui/login/loginname", with(base, "loginName", e2eUser))
	if cs := extractInput(page, "gorilla.csrf.Token"); cs != "" {
		base.Set("gorilla.csrf.Token", cs)
	}
	st, hdr, page = formPost("/ui/login/password", with(base, "password", e2ePass))
	if cs := extractInput(page, "gorilla.csrf.Token"); cs != "" {
		base.Set("gorilla.csrf.Token", cs)
	}
	if st != 302 && strings.Contains(string(page), "/ui/login/mfa/prompt") {
		st, hdr, _ = formPost("/ui/login/mfa/prompt", with(base, "skip", "true"))
	}
	if st == 302 && strings.Contains(hdr.Get("Location"), "/oauth/v2/authorize/callback") {
		loc := hdr.Get("Location")
		if strings.HasPrefix(loc, "http") {
			st, hdr, _ = get(t, c, loc, nil)
		} else {
			st, hdr, _ = get(t, c, authURL+loc, nil)
		}
	}
	cb := hdr.Get("Location")
	if !strings.Contains(cb, "code=") {
		t.Fatalf("no code in callback: %s", cb)
	}

	// 3. app callback with the same jar (state cookie) -> session cookie
	st, hdr, _ = get(t, c, cb, nil)
	t.Logf("app callback: %d -> %s", st, hdr.Get("Location"))

	// 4. repositories page
	st, _, html := get(t, c, appURL+"/repositories", nil)
	if st != 200 {
		t.Fatalf("/repositories: %d (redirect to %s)", st, hdr.Get("Location"))
	}
	body := string(html)
	fmt.Printf("REPOS PAGE: rows=%d mentions-e2e-git=%v\n", strings.Count(body, "row-item"), strings.Contains(body, "e2e-git"))
	if !strings.Contains(body, "e2e-git") {
		t.Errorf("repositories page does not render repos (len=%d)", len(body))
	}
}

// TestCleanupE2E deletes the e2e tester account, which purges its owned
// projects/repos/orgs (GDPR account deletion path).
func TestCleanupE2E(t *testing.T) {
	requireLive(t)
	token := login(t)
	st, b := apiCall(t, token, http.MethodDelete, "/auth/me", nil)
	fmt.Printf("DELETE /auth/me: %d %.200s\n", st, b)
	if st >= 500 {
		t.Errorf("delete account: %d %s", st, b)
	}
}
