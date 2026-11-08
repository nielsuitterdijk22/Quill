package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nielsuitterdijk22/quill/internal/config"
	"github.com/nielsuitterdijk22/quill/internal/logging"
)

// ghFlowTestServer builds a Server configured like production for the GitHub
// import OAuth flow: Zitadel-shaped auth, CORS origin on the app host, and a
// GitHub OAuth client. The store is nil — the OAuth start handler doesn't touch
// the database.
func ghFlowTestServer(t *testing.T, env, cookieDomain string) *Server {
	t.Helper()
	cfg := &config.Config{
		Env:                env,
		JWT:                config.JWTConfig{Secret: "test-secret"},
		CORSAllowedOrigins: []string{"https://app.example.com"},
		CookieDomain:       cookieDomain,
		GitHub:             config.GitHubConfig{ClientID: "gh-client-id"},
		Forgejo:            config.ForgejoConfig{BaseURL: "http://forgejo.example.com"},
	}
	return New(cfg, logging.New("error", "text"), nil)
}

// flowCookieFor extracts the named Set-Cookie header from a recorder, or fails
// the test when the handler did not set it.
func flowCookieFor(t *testing.T, rec *httptest.ResponseRecorder, name string) string {
	t.Helper()
	for _, c := range rec.Result().Cookies() {
		if c.Name == name {
			return c.String()
		}
	}
	t.Fatalf("cookie %q not set in response", name)
	return ""
}

func TestGitHubOAuthStartCookieHasDomain(t *testing.T) {
	srv := ghFlowTestServer(t, "production", "example.com")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/github", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d", rec.Code)
	}
	state := flowCookieFor(t, rec, githubStateCookie)
	if !strings.Contains(state, "Domain=example.com") {
		t.Fatalf("state cookie should carry Domain=example.com, got: %q", state)
	}
	if !strings.Contains(state, "Secure") {
		t.Fatalf("state cookie should be Secure in production, got: %q", state)
	}
	if !strings.Contains(state, "HttpOnly") {
		t.Fatalf("state cookie should be HttpOnly, got: %q", state)
	}
}

func TestGitHubOAuthStartCookieHostOnlyWithoutDomain(t *testing.T) {
	// Development (localhost via the frontend rewrite proxy) must keep
	// host-only cookies — a Domain attribute is neither needed nor valid for
	// single-label hosts.
	srv := ghFlowTestServer(t, "development", "")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/github", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d", rec.Code)
	}
	state := flowCookieFor(t, rec, githubStateCookie)
	if strings.Contains(state, "Domain=") {
		t.Fatalf("state cookie must stay host-only in dev, got: %q", state)
	}
}

func TestFlowCookieAttrs(t *testing.T) {
	srv := ghFlowTestServer(t, "production", "example.com")

	c := srv.flowCookie(githubTokenCookie, "value", 900)
	if c.Domain != "example.com" {
		t.Fatalf("Domain = %q, want example.com", c.Domain)
	}
	if !c.Secure || !c.HttpOnly || c.SameSite != http.SameSiteLaxMode || c.Path != "/" {
		t.Fatalf("unexpected cookie attrs: %+v", c)
	}

	// Clearing still needs the Domain so the browser deletes the domain-wide
	// cookie rather than setting a host-only expiry cookie.
	clear := srv.flowCookie(githubTokenCookie, "", -1)
	if clear.Domain != "example.com" {
		t.Fatalf("clear cookie Domain = %q, want example.com", clear.Domain)
	}
}
