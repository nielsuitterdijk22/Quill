package projectsync

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nielsuitterdijk22/quill/internal/config"
)

// fakeZitadel is an httptest-backed stand-in for a Zitadel instance: it serves
// the OIDC discovery document and the client_credentials token endpoint, counts
// hits to each, and captures what the last token request presented.
type fakeZitadel struct {
	mu            sync.Mutex
	discoveryHits int
	tokenHits     int

	gotGrantType string
	gotScope     string
	gotUser      string
	gotPass      string

	// ttl is the expires_in (seconds) returned by the token endpoint.
	ttl int64
	// failToken makes the token endpoint return 401 to exercise error handling.
	failToken bool
	// tokenDelay widens the token-endpoint window so concurrent callers overlap.
	tokenDelay time.Duration

	server *httptest.Server
}

func newFakeZitadel() *fakeZitadel {
	z := &fakeZitadel{ttl: 3600}
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		z.mu.Lock()
		z.discoveryHits++
		z.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"issuer":"` + z.server.URL + `","token_endpoint":"` + z.server.URL + `/oauth/v2/token"}`))
	})
	mux.HandleFunc("/oauth/v2/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		user, pass, _ := r.BasicAuth()
		z.mu.Lock()
		z.tokenHits++
		z.gotGrantType = r.PostForm.Get("grant_type")
		z.gotScope = r.PostForm.Get("scope")
		z.gotUser = user
		z.gotPass = pass
		delay := z.tokenDelay
		fail := z.failToken
		ttl := z.ttl
		z.mu.Unlock()

		if delay > 0 {
			time.Sleep(delay)
		}
		if fail {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"invalid_client"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"minted-token","token_type":"Bearer","expires_in":` + strconv.FormatInt(ttl, 10) + `}`))
	})
	z.server = httptest.NewServer(mux)
	return z
}

func (z *fakeZitadel) close() { z.server.Close() }

func (z *fakeZitadel) counts() (discovery, token int) {
	z.mu.Lock()
	defer z.mu.Unlock()
	return z.discoveryHits, z.tokenHits
}

const (
	testClientID  = "tempo-machine"
	testSecret    = "s3cr3t-value"
	testProjectID = "998877665544332211"
)

func newTestTokenSource(z *fakeZitadel, clk *clock) *ZitadelTokenSource {
	ts := NewZitadelTokenSource(ZitadelTokenConfig{
		Issuer:       z.server.URL,
		ClientID:     testClientID,
		ClientSecret: testSecret,
		ProjectID:    testProjectID,
	})
	if clk != nil {
		ts.now = clk.now
	}
	return ts
}

func TestZitadelTokenRequestShape(t *testing.T) {
	z := newFakeZitadel()
	defer z.close()

	ts := newTestTokenSource(z, nil)
	tok, err := ts.Token(context.Background())
	if err != nil {
		t.Fatalf("Token: %v", err)
	}
	if tok != "minted-token" {
		t.Fatalf("expected minted-token, got %q", tok)
	}

	z.mu.Lock()
	defer z.mu.Unlock()
	if z.gotGrantType != "client_credentials" {
		t.Fatalf("grant_type = %q, want client_credentials", z.gotGrantType)
	}
	wantScope := "openid urn:zitadel:iam:org:project:id:" + testProjectID + ":aud"
	if z.gotScope != wantScope {
		t.Fatalf("scope = %q, want %q", z.gotScope, wantScope)
	}
	if !strings.Contains(z.gotScope, "urn:zitadel:iam:org:project:id:"+testProjectID+":aud") {
		t.Fatalf("scope missing reserved aud scope for project: %q", z.gotScope)
	}
	// client_secret_basic: the credentials arrive in the Basic auth header.
	if z.gotUser != testClientID || z.gotPass != testSecret {
		t.Fatalf("basic auth = %q:%q, want %q:%q", z.gotUser, z.gotPass, testClientID, testSecret)
	}
}

func TestZitadelTokenAuthorizationHeaderIsBasic(t *testing.T) {
	// Assert the raw header is Basic base64(client_id:client_secret), independent
	// of the server's BasicAuth parsing.
	var gotHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "openid-configuration") {
			w.Write([]byte(`{"token_endpoint":"` + baseURL(r) + `/oauth/v2/token"}`))
			return
		}
		gotHeader = r.Header.Get("Authorization")
		w.Write([]byte(`{"access_token":"t","expires_in":3600}`))
	}))
	defer srv.Close()

	ts := NewZitadelTokenSource(ZitadelTokenConfig{Issuer: srv.URL, ClientID: testClientID, ClientSecret: testSecret, ProjectID: testProjectID})
	if _, err := ts.Token(context.Background()); err != nil {
		t.Fatalf("Token: %v", err)
	}
	const prefix = "Basic "
	if !strings.HasPrefix(gotHeader, prefix) {
		t.Fatalf("Authorization = %q, want Basic scheme", gotHeader)
	}
	dec, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(gotHeader, prefix))
	if err != nil {
		t.Fatalf("decode basic: %v", err)
	}
	if string(dec) != testClientID+":"+testSecret {
		t.Fatalf("basic creds = %q, want %q", dec, testClientID+":"+testSecret)
	}
}

func baseURL(r *http.Request) string {
	return "http://" + r.Host
}

func TestZitadelTokenCachesWithinTTL(t *testing.T) {
	z := newFakeZitadel()
	defer z.close()
	clk := newClock()
	ts := newTestTokenSource(z, clk)

	for i := 0; i < 3; i++ {
		if _, err := ts.Token(context.Background()); err != nil {
			t.Fatalf("Token #%d: %v", i, err)
		}
	}
	discovery, token := z.counts()
	if token != 1 {
		t.Fatalf("expected 1 token fetch within TTL, got %d", token)
	}
	if discovery != 1 {
		t.Fatalf("expected discovery fetched once and cached, got %d", discovery)
	}
}

func TestZitadelTokenProactiveRefresh(t *testing.T) {
	z := newFakeZitadel()
	defer z.close()
	clk := newClock()
	ts := newTestTokenSource(z, clk)

	if _, err := ts.Token(context.Background()); err != nil {
		t.Fatalf("initial Token: %v", err)
	}
	if _, token := z.counts(); token != 1 {
		t.Fatalf("expected 1 fetch, got %d", token)
	}

	// Advance to just inside the refresh skew window (ttl=3600, skew=60): the
	// cached token is now considered stale and must be re-fetched.
	clk.advance(3600*time.Second - 30*time.Second)
	if _, err := ts.Token(context.Background()); err != nil {
		t.Fatalf("refresh Token: %v", err)
	}
	if _, token := z.counts(); token != 2 {
		t.Fatalf("expected proactive refresh (2 fetches), got %d", token)
	}

	// Still well before the new token's skew window: served from cache.
	clk.advance(10 * time.Second)
	if _, err := ts.Token(context.Background()); err != nil {
		t.Fatalf("cached Token: %v", err)
	}
	if _, token := z.counts(); token != 2 {
		t.Fatalf("expected no extra fetch, got %d", token)
	}
}

func TestZitadelTokenErrorSurfaces(t *testing.T) {
	z := newFakeZitadel()
	defer z.close()
	z.mu.Lock()
	z.failToken = true
	z.mu.Unlock()

	ts := newTestTokenSource(z, nil)
	tok, err := ts.Token(context.Background())
	if err == nil {
		t.Fatalf("expected error on 401, got token %q", tok)
	}
	if !strings.Contains(err.Error(), "401") {
		t.Fatalf("error should mention status: %v", err)
	}
}

func TestZitadelTokenDiscoveryErrorSurfaces(t *testing.T) {
	// Point at a closed server so both discovery and token requests fail at the
	// transport level; the error must propagate (dispatcher retries the delivery).
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close()

	ts := NewZitadelTokenSource(ZitadelTokenConfig{Issuer: url, ClientID: testClientID, ClientSecret: testSecret, ProjectID: testProjectID})
	if _, err := ts.Token(context.Background()); err == nil {
		t.Fatal("expected error when Zitadel is unreachable")
	}
}

func TestZitadelTokenNoStampede(t *testing.T) {
	z := newFakeZitadel()
	z.mu.Lock()
	z.tokenDelay = 50 * time.Millisecond // widen the window so callers overlap
	z.mu.Unlock()
	defer z.close()

	ts := newTestTokenSource(z, nil)

	const goroutines = 20
	var wg sync.WaitGroup
	errs := make(chan error, goroutines)
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			if _, err := ts.Token(context.Background()); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent Token: %v", err)
	}
	if _, token := z.counts(); token != 1 {
		t.Fatalf("concurrent callers must not stampede: expected 1 token fetch, got %d", token)
	}
}

func TestSelectTempoTokenSource(t *testing.T) {
	// Fully-configured Zitadel machine user -> ZitadelTokenSource.
	zcfg := config.TempoSyncConfig{
		Token:               "static-fallback",
		ZitadelIssuer:       "https://auth.example.com",
		ZitadelClientID:     "cid",
		ZitadelClientSecret: "secret",
		ZitadelProjectID:    "12345",
	}
	if src := SelectTempoTokenSource(zcfg); func() bool { _, ok := src.(*ZitadelTokenSource); return !ok }() {
		t.Fatalf("expected *ZitadelTokenSource when fully configured, got %T", src)
	}

	// Partial Zitadel config -> falls back to the static escape hatch.
	partial := config.TempoSyncConfig{Token: "static-fallback", ZitadelIssuer: "https://auth.example.com"}
	if src := SelectTempoTokenSource(partial); func() bool { _, ok := src.(StaticTokenSource); return !ok }() {
		t.Fatalf("partial Zitadel config must fall back to StaticTokenSource, got %T", src)
	}

	// No Zitadel config -> StaticTokenSource carrying the token verbatim.
	static := config.TempoSyncConfig{Token: "static-fallback"}
	src := SelectTempoTokenSource(static)
	st, ok := src.(StaticTokenSource)
	if !ok {
		t.Fatalf("expected StaticTokenSource, got %T", src)
	}
	if got, _ := st.Token(context.Background()); got != "static-fallback" {
		t.Fatalf("static source token = %q, want static-fallback", got)
	}
}
