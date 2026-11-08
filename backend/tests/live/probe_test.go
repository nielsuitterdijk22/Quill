package live

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
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

// webSession drives the full app signin flow (app -> Zitadel -> callback) and
// returns a client holding the NextAuth session cookie.
func webSession(t *testing.T) *http.Client {
	t.Helper()
	c := client(t)
	_, _, b := get(t, c, appURL+"/api/auth/csrf", nil)
	csrf := jbody(t, b)["csrfToken"].(string)
	preq, _ := http.NewRequest(http.MethodPost, appURL+"/api/auth/signin/zitadel", strings.NewReader(url.Values{"csrfToken": {csrf}, "callbackUrl": {appURL + "/"}}.Encode()))
	preq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.Do(preq)
	if err != nil || resp.StatusCode != 302 {
		t.Fatalf("app signin: %v %d", err, resp.StatusCode)
	}
	authReq := resp.Header.Get("Location")
	resp.Body.Close()

	st, hdr, _ := get(t, c, authReq, nil)
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
		pq, _ := http.NewRequest(http.MethodPost, authURL+path, strings.NewReader(fields.Encode()))
		pq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r, err := c.Do(pq)
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
		t.Fatalf("no code: %s", cb)
	}
	if st, _, _ := get(t, c, cb, nil); st != 302 && st != 200 {
		t.Fatalf("app callback: %d", st)
	}
	return c
}

// pushBranch clones the repo with a fresh git token, writes content, and
// pushes the given branch.
func pushBranch(t *testing.T, token, proj, repo, branch, filename, content string) {
	t.Helper()
	st, b := apiCall(t, token, http.MethodPost, "/me/git-token", map[string]any{"name": "e2e"})
	if st != 200 && st != 201 {
		t.Fatalf("git token: %d %s", st, b)
	}
	cred := jbody(t, b)
	tok, _ := cred["token"].(string)
	username, _ := cred["username"].(string)
	dir := t.TempDir()
	sh(t, dir, "git", "clone", fmt.Sprintf("https://%s:%s@git.black-tulip.nl/%s/%s.git", username, tok, proj, repo), "r")
	rd := dir + "/r"
	sh(t, rd, "git", "config", "user.email", "e2e@black-tulip.nl")
	sh(t, rd, "git", "config", "user.name", "e2e")
	if branch != "main" {
		sh(t, rd, "git", "checkout", "-b", branch)
	}
	if err := os.WriteFile(rd+"/"+filename, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	sh(t, rd, "git", "add", filename)
	sh(t, rd, "git", "commit", "-m", "e2e "+filename)
	sh(t, rd, "git", "push", "-u", "origin", branch)
}
