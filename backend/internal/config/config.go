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

	JWT     JWTConfig
	Forgejo ForgejoConfig

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
		JWT: JWTConfig{
			Secret: getenv("QUILL_JWT_SECRET", ""),
			Issuer: getenv("QUILL_JWT_ISSUER", "quill"),
			TTL:    getdur("QUILL_JWT_TTL", 24*time.Hour),
		},
		Forgejo: ForgejoConfig{
			BaseURL:    getenv("QUILL_FORGEJO_BASE_URL", "http://localhost:3000"),
			AdminToken: getenv("QUILL_FORGEJO_ADMIN_TOKEN", ""),
			PublicURL:  getenv("QUILL_FORGEJO_PUBLIC_URL", ""),
		},
		CORSAllowedOrigins: getlist("QUILL_CORS_ALLOWED_ORIGINS", []string{"http://localhost:3001"}),
	}

	if cfg.IsProduction() && cfg.JWT.Secret == "" {
		return nil, fmt.Errorf("QUILL_JWT_SECRET is required when QUILL_ENV=production")
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
