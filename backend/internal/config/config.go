// Package config loads Quill backend configuration from environment variables.
//
// All settings have development-friendly defaults so the server can boot with no
// configuration. Production deployments must set secrets explicitly; Load returns
// an error when required secrets are missing in production.
package config

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// Config holds all runtime configuration for the backend.
type Config struct {
	Env      string
	HTTPAddr string

	LogLevel  string
	LogFormat string

	ReadTimeout  time.Duration
	WriteTimeout time.Duration

	DatabaseURL string

	// AuthProvider selects the external identity provider. Only "zitadel" is
	// supported; the matching provider config below must be populated. Local
	// username/password auth is always available regardless of this setting.
	AuthProvider string

	JWT       JWTConfig
	Zitadel   ZitadelConfig
	GitHub    GitHubConfig
	Forgejo   ForgejoConfig
	Pipeline  PipelineConfig
	TempoSync TempoSyncConfig

	// WebhookSecret authenticates inbound Forgejo webhooks that auto-trigger
	// pipelines. When empty, signature verification is skipped (dev mode), so set
	// it in any shared environment.
	WebhookSecret string

	CORSAllowedOrigins []string
}

// JWTConfig configures Quill-issued access tokens (used from PR 3 onward).
type JWTConfig struct {
	Secret string
	Issuer string
	TTL    time.Duration
}

// ForgejoConfig points at the wrapped Forgejo instance (used from PR 4 onward).
type ForgejoConfig struct {
	BaseURL    string
	AdminToken string
	// PublicURL is the externally-accessible Forgejo URL shown to users in clone
	// instructions. Defaults to BaseURL when not set.
	PublicURL string
}

// ZitadelConfig holds settings for self-hosted Zitadel OIDC authentication.
type ZitadelConfig struct {
	// Issuer is the Zitadel instance base URL (e.g. "https://auth.example.com").
	// It is the JWT issuer; the JWKS lives at <Issuer>/oauth/v2/keys.
	Issuer string
	// ManagementToken is a Zitadel service-account token (PAT or machine key JWT)
	// used for the Management API: deleting a user on account deletion (to stop
	// session resurrection) and, later, org/member provisioning. Optional — when
	// empty, those server-side calls are skipped and logged.
	ManagementToken string
}

// GitHubConfig holds credentials for the GitHub OAuth integration used during
// onboarding to import existing repositories.
type GitHubConfig struct {
	ClientID     string
	ClientSecret string
}

// TempoSyncConfig controls the outbound pushes to Tempo (see the tight
// Quill/Tempo integration design). When a URL is empty, that push is disabled and
// its background dispatcher stays idle.
type TempoSyncConfig struct {
	// URL is Tempo's project-mirror intake endpoint. Empty disables project sync.
	URL string
	// RefsURL is Tempo's work-item-refs endpoint (PR 8.4), which the Forgejo
	// webhook handler pushes commit/PR cross-links to (PR 8.5). Empty disables the
	// feature entirely: the webhook path is completely unaffected.
	RefsURL string
	// Token is the static bearer token presented to Tempo for both pushes. It is
	// the escape hatch used when the Zitadel machine-user config below is not fully
	// set: the dispatchers acquire their token through an interface, so the Zitadel
	// client-credentials machine token can replace this without code changes. Empty
	// sends requests unauthenticated (local dev only).
	Token string

	// The four fields below configure the outbound Zitadel client-credentials
	// machine user Quill authenticates as when pushing to Tempo. When all are set,
	// the dispatchers mint a short-lived Zitadel access token per delivery instead
	// of using the static Token above. This is the outbound counterpart to
	// ZitadelConfig (which governs inbound user login) — deliberately a separate
	// concern with its own credentials, so machine-to-machine auth and user login
	// rotate independently.

	// ZitadelIssuer is the Zitadel instance base URL (e.g.
	// "https://auth.example.com"). Its OIDC discovery document
	// (<Issuer>/.well-known/openid-configuration) yields the token endpoint.
	ZitadelIssuer string
	// ZitadelClientID is the machine user's OAuth2 client id (client_secret_basic).
	ZitadelClientID string
	// ZitadelClientSecret is the machine user's OAuth2 client secret.
	ZitadelClientSecret string
	// ZitadelProjectID is the Zitadel project id whose apps include Tempo. It is
	// used to build Zitadel's reserved audience scope
	// urn:zitadel:iam:org:project:id:<ZitadelProjectID>:aud so the minted token is
	// accepted by Tempo as its intended audience.
	ZitadelProjectID string
}

// ZitadelEnabled reports whether the outbound Zitadel machine-user credentials
// are fully configured. When true the dispatchers use a Zitadel
// client-credentials TokenSource; when false they fall back to the static Token.
// All four fields are required — a partial config is treated as unset so a
// half-populated deployment fails closed onto the static escape hatch rather than
// emitting broken token requests.
func (c TempoSyncConfig) ZitadelEnabled() bool {
	return c.ZitadelIssuer != "" &&
		c.ZitadelClientID != "" &&
		c.ZitadelClientSecret != "" &&
		c.ZitadelProjectID != ""
}

// PipelineConfig controls how workflow runs are dispatched.
type PipelineConfig struct {
	// DispatchURL points at the standalone pipeline dispatcher. When empty, the
	// API falls back to the in-process act runner for tests and simple dev setups.
	DispatchURL string
	// DispatchSecret signs API -> dispatcher requests. Leave empty only for local
	// development on a trusted Docker network.
	DispatchSecret string
}

// Load reads configuration from the environment.
func Load() (*Config, error) {
	cfg := &Config{
		Env:          getenv("QUILL_ENV", "development"),
		HTTPAddr:     getenv("QUILL_HTTP_ADDR", ":8080"),
		LogLevel:     getenv("QUILL_LOG_LEVEL", "info"),
		LogFormat:    getenv("QUILL_LOG_FORMAT", "json"),
		ReadTimeout:  getdur("QUILL_HTTP_READ_TIMEOUT", 15*time.Second),
		WriteTimeout: getdur("QUILL_HTTP_WRITE_TIMEOUT", 30*time.Second),
		DatabaseURL:  getenv("QUILL_DATABASE_URL", "postgres://quill:quill@localhost:5432/quill?sslmode=disable"),
		AuthProvider: strings.ToLower(getenv("QUILL_AUTH_PROVIDER", "zitadel")),
		JWT: JWTConfig{
			Secret: getenv("QUILL_JWT_SECRET", ""),
			Issuer: getenv("QUILL_JWT_ISSUER", "quill"),
			TTL:    getdur("QUILL_JWT_TTL", 24*time.Hour),
		},
		Zitadel: ZitadelConfig{
			Issuer:          strings.TrimSuffix(getenv("ZITADEL_ISSUER", ""), "/"),
			ManagementToken: getenv("ZITADEL_MANAGEMENT_TOKEN", ""),
		},
		GitHub: GitHubConfig{
			ClientID:     getenv("GITHUB_CLIENT_ID", ""),
			ClientSecret: getenv("GITHUB_CLIENT_SECRET", ""),
		},
		Forgejo: ForgejoConfig{
			BaseURL:    getenv("FORGEJO_BASE_URL", "http://localhost:3000"),
			AdminToken: getenv("FORGEJO_ADMIN_TOKEN", ""),
			PublicURL:  getenv("FORGEJO_PUBLIC_URL", ""),
		},
		Pipeline: PipelineConfig{
			DispatchURL:    getenv("QUILL_PIPELINE_DISPATCH_URL", ""),
			DispatchSecret: getenv("QUILL_PIPELINE_DISPATCH_SECRET", ""),
		},
		TempoSync: TempoSyncConfig{
			URL:                 getenv("QUILL_TEMPO_SYNC_URL", ""),
			RefsURL:             getenv("QUILL_TEMPO_SYNC_REFS_URL", ""),
			Token:               getenv("QUILL_TEMPO_SYNC_TOKEN", ""),
			ZitadelIssuer:       strings.TrimSuffix(getenv("QUILL_TEMPO_SYNC_ZITADEL_ISSUER", ""), "/"),
			ZitadelClientID:     getenv("QUILL_TEMPO_SYNC_ZITADEL_CLIENT_ID", ""),
			ZitadelClientSecret: getenv("QUILL_TEMPO_SYNC_ZITADEL_CLIENT_SECRET", ""),
			ZitadelProjectID:    getenv("QUILL_TEMPO_SYNC_ZITADEL_PROJECT_ID", ""),
		},
		WebhookSecret:      getenv("QUILL_WEBHOOK_SECRET", ""),
		CORSAllowedOrigins: getlist("QUILL_CORS_ALLOWED_ORIGINS", []string{"http://localhost:3001"}),
	}

	if cfg.IsProduction() && cfg.JWT.Secret == "" && cfg.Zitadel.Issuer == "" {
		return nil, fmt.Errorf("production requires QUILL_JWT_SECRET (local auth) or ZITADEL_ISSUER (Zitadel)")
	}

	return cfg, nil
}

// IsProduction reports whether the server runs in a production environment.
func (c *Config) IsProduction() bool { return strings.EqualFold(c.Env, "production") }

func getenv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}

func getdur(key string, fallback time.Duration) time.Duration {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return fallback
}

func getlist(key string, fallback []string) []string {
	v, ok := os.LookupEnv(key)
	if !ok || strings.TrimSpace(v) == "" {
		return fallback
	}
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	if len(out) == 0 {
		return fallback
	}
	return out
}
