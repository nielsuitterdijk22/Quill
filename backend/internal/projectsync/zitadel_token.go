package projectsync

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/nielsuitterdijk22/quill/internal/config"
)

// zitadelHTTPTimeout bounds a single discovery or token request. Kept short: a
// slow token endpoint must surface as a delivery error the dispatcher retries,
// not hang the poll loop.
const zitadelHTTPTimeout = 15 * time.Second

// zitadelRefreshSkew is how far before real expiry the cached token is treated
// as stale. We refresh proactively rather than at the exact expiry so a token is
// never presented to Tempo with only milliseconds of validity left: 60s absorbs
// clock skew between Quill/Zitadel/Tempo plus the round-trip latency of the
// delivery request the token is minted for.
const zitadelRefreshSkew = 60 * time.Second

// ZitadelTokenConfig holds the outbound machine-user credentials the
// ZitadelTokenSource authenticates with. It mirrors the QUILL_TEMPO_SYNC_ZITADEL_*
// config fields but is decoupled from the config package so the token source can
// be constructed directly in tests.
type ZitadelTokenConfig struct {
	// Issuer is the Zitadel instance base URL; its OIDC discovery document yields
	// the token endpoint.
	Issuer string
	// ClientID / ClientSecret are the machine user's client_secret_basic credentials.
	ClientID     string
	ClientSecret string
	// ProjectID is the Zitadel project id whose apps include Tempo, used to build
	// the reserved audience scope.
	ProjectID string
}

// ZitadelTokenSource acquires a Tempo-audience access token from Zitadel via the
// OAuth2 client_credentials grant and caches it until shortly before expiry. It
// implements TokenSource, so it drops into either dispatcher in place of
// StaticTokenSource. All fetches are serialized by mu, which also guarantees a
// burst of concurrent Token() callers triggers at most one token request.
type ZitadelTokenSource struct {
	issuer       string
	clientID     string
	clientSecret string
	// scope is the full space-delimited scope string sent on every token request,
	// including Zitadel's reserved project-audience scope.
	scope  string
	client *http.Client
	// now is overridable in tests to drive proactive-refresh timing deterministically.
	now func() time.Time

	mu sync.Mutex
	// tokenEndpoint is resolved once from OIDC discovery and cached for the process
	// lifetime (the discovery document is effectively static per issuer).
	tokenEndpoint string
	accessToken   string
	expiresAt     time.Time
}

// NewZitadelTokenSource builds a token source from the outbound machine-user
// config. The token endpoint is discovered lazily on the first Token() call so
// construction never blocks on network I/O.
func NewZitadelTokenSource(cfg ZitadelTokenConfig) *ZitadelTokenSource {
	return &ZitadelTokenSource{
		issuer:       strings.TrimSuffix(cfg.Issuer, "/"),
		clientID:     cfg.ClientID,
		clientSecret: cfg.ClientSecret,
		scope:        buildTempoScope(cfg.ProjectID),
		client:       &http.Client{Timeout: zitadelHTTPTimeout},
		now:          func() time.Time { return time.Now().UTC() },
	}
}

// buildTempoScope returns the space-delimited scope requested from Zitadel:
// "openid" plus Zitadel's reserved audience scope binding the token's aud to the
// Tempo project, so Tempo accepts it as its intended audience.
func buildTempoScope(projectID string) string {
	return "openid urn:zitadel:iam:org:project:id:" + projectID + ":aud"
}

// Token returns a valid bearer access token, refreshing from Zitadel when the
// cache is empty or within zitadelRefreshSkew of expiry. A refresh failure is
// returned as an error (not a stale token) so the dispatcher reschedules the
// whole delivery and retries later. The mutex is held across the network fetch:
// dispatcher call volume is low, and serializing guarantees no token-endpoint
// stampede when both dispatchers wake at once.
func (s *ZitadelTokenSource) Token(ctx context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.accessToken != "" && s.now().Before(s.expiresAt.Add(-zitadelRefreshSkew)) {
		return s.accessToken, nil
	}
	if err := s.refreshLocked(ctx); err != nil {
		return "", err
	}
	return s.accessToken, nil
}

// refreshLocked performs the client_credentials grant and updates the cache. The
// caller must hold s.mu.
func (s *ZitadelTokenSource) refreshLocked(ctx context.Context) error {
	endpoint, err := s.resolveTokenEndpointLocked(ctx)
	if err != nil {
		return err
	}

	form := url.Values{}
	form.Set("grant_type", "client_credentials")
	form.Set("scope", s.scope)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("build token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	// client_secret_basic: credentials go in the Authorization: Basic header.
	req.SetBasicAuth(url.QueryEscape(s.clientID), url.QueryEscape(s.clientSecret))

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("zitadel token request: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("zitadel token endpoint returned status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var tok struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
		ExpiresIn   int64  `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &tok); err != nil {
		return fmt.Errorf("decode zitadel token response: %w", err)
	}
	if tok.AccessToken == "" {
		return fmt.Errorf("zitadel token response missing access_token")
	}

	s.accessToken = tok.AccessToken
	// Default to a short TTL if the server omits expires_in so we never cache a
	// token indefinitely.
	ttl := time.Duration(tok.ExpiresIn) * time.Second
	if ttl <= 0 {
		ttl = zitadelRefreshSkew
	}
	s.expiresAt = s.now().Add(ttl)
	return nil
}

// oidcDiscovery is the subset of the OIDC discovery document we need.
type oidcDiscovery struct {
	TokenEndpoint string `json:"token_endpoint"`
}

// resolveTokenEndpointLocked returns the cached token endpoint, fetching it from
// the issuer's OIDC discovery document on first use. The caller must hold s.mu.
func (s *ZitadelTokenSource) resolveTokenEndpointLocked(ctx context.Context) (string, error) {
	if s.tokenEndpoint != "" {
		return s.tokenEndpoint, nil
	}
	discoveryURL := s.issuer + "/.well-known/openid-configuration"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, discoveryURL, nil)
	if err != nil {
		return "", fmt.Errorf("build discovery request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("zitadel discovery request: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("zitadel discovery returned status %d", resp.StatusCode)
	}
	var doc oidcDiscovery
	if err := json.Unmarshal(body, &doc); err != nil {
		return "", fmt.Errorf("decode zitadel discovery: %w", err)
	}
	if doc.TokenEndpoint == "" {
		return "", fmt.Errorf("zitadel discovery missing token_endpoint")
	}
	s.tokenEndpoint = doc.TokenEndpoint
	return s.tokenEndpoint, nil
}

// SelectTempoTokenSource chooses the outbound auth strategy for the Tempo
// dispatchers from config: the Zitadel client-credentials machine token when the
// QUILL_TEMPO_SYNC_ZITADEL_* credentials are fully set, otherwise the static
// QUILL_TEMPO_SYNC_TOKEN (which may itself be empty for unauthenticated local
// dev). Both dispatchers are expected to share the returned source.
func SelectTempoTokenSource(cfg config.TempoSyncConfig) TokenSource {
	if cfg.ZitadelEnabled() {
		return NewZitadelTokenSource(ZitadelTokenConfig{
			Issuer:       cfg.ZitadelIssuer,
			ClientID:     cfg.ZitadelClientID,
			ClientSecret: cfg.ZitadelClientSecret,
			ProjectID:    cfg.ZitadelProjectID,
		})
	}
	return StaticTokenSource(cfg.Token)
}
