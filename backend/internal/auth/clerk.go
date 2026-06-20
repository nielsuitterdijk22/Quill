package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/lestrrat-go/jwx/v3/jwk"
	"github.com/lestrrat-go/jwx/v3/jws"
	"github.com/lestrrat-go/jwx/v3/jwt"

	"github.com/nielsuitterdijk22/quill/internal/config"
	"github.com/nielsuitterdijk22/quill/internal/forgejo"
	"github.com/nielsuitterdijk22/quill/internal/store"
	"github.com/nielsuitterdijk22/quill/internal/store/db"
)

// ClerkVerifier verifies Clerk-issued RS256 JWTs, caches the JWKS in memory,
// and provisions Quill users and tenants on first login.
type ClerkVerifier struct {
	store       *store.Store
	forgejo     *forgejo.Client
	logger      *slog.Logger
	frontendAPI string
	secretKey   string
	jwksURL     string
	mu          sync.RWMutex
	keySet      jwk.Set
}

// NewClerkVerifier creates a ClerkVerifier from configuration. Call Start to
// fetch the initial JWKS and begin background refresh.
func NewClerkVerifier(cfg config.ClerkConfig, st *store.Store, logger *slog.Logger) *ClerkVerifier {
	frontendAPI := strings.TrimSuffix(cfg.FrontendAPI, "/")
	return &ClerkVerifier{
		store:       st,
		logger:      logger,
		frontendAPI: frontendAPI,
		secretKey:   cfg.SecretKey,
		jwksURL:     frontendAPI + "/.well-known/jwks.json",
	}
}

// WithForgejo enables best-effort Forgejo user provisioning on first Clerk
// login and returns the verifier for chaining.
func (v *ClerkVerifier) WithForgejo(fj *forgejo.Client) *ClerkVerifier {
	v.forgejo = fj
	return v
}

// Enabled reports whether Clerk authentication is configured.
func (v *ClerkVerifier) Enabled() bool { return v.frontendAPI != "" }

// Start fetches the initial JWKS synchronously and begins a background refresh
// every 15 minutes. ctx is used for the refresh goroutine's lifetime.
func (v *ClerkVerifier) Start(ctx context.Context) {
	if !v.Enabled() {
		return
	}
	if err := v.refresh(ctx); err != nil {
		v.logger.Warn("initial JWKS fetch failed — auth will fail until retry", "error", err)
	}
	go func() {
		t := time.NewTicker(15 * time.Minute)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if err := v.refresh(context.WithoutCancel(ctx)); err != nil {
					v.logger.Warn("JWKS refresh failed", "error", err)
				}
			}
		}
	}()
}

func (v *ClerkVerifier) refresh(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	set, err := jwk.Fetch(ctx, v.jwksURL)
	if err != nil {
		return fmt.Errorf("jwks fetch %s: %w", v.jwksURL, err)
	}
	v.mu.Lock()
	v.keySet = set
	v.mu.Unlock()
	return nil
}

// Verify validates a Clerk JWT, resolves (or provisions) the Quill user and
// tenant, and returns the resulting Identity.
func (v *ClerkVerifier) Verify(ctx context.Context, token string) (Identity, error) {
	v.mu.RLock()
	ks := v.keySet
	v.mu.RUnlock()
	if ks == nil {
		// Startup race or persistent fetch failure — try once more inline.
		if err := v.refresh(ctx); err != nil {
			return Identity{}, ErrInvalidCredentials
		}
		v.mu.RLock()
		ks = v.keySet
		v.mu.RUnlock()
	}

	tok, err := jwt.Parse(
		[]byte(token),
		jwt.WithKeySet(ks, jws.WithInferAlgorithmFromKey(true)),
		jwt.WithIssuer(v.frontendAPI),
		jwt.WithValidate(true),
		jwt.WithAcceptableSkew(30*time.Second),
	)
	if err != nil {
		return Identity{}, ErrInvalidCredentials
	}

	sub, ok := tok.Subject()
	if !ok || sub == "" {
		return Identity{}, ErrInvalidCredentials
	}

	var orgID, orgSlug string
	_ = tok.Get("org_id", &orgID)
	_ = tok.Get("org_slug", &orgSlug)

	return v.resolveIdentity(ctx, sub, orgID, orgSlug)
}

// resolveIdentity looks up or provisions the Quill user and tenant for a
// Clerk (clerkUserID, orgID) pair.
func (v *ClerkVerifier) resolveIdentity(ctx context.Context, clerkUserID, orgID, orgSlug string) (Identity, error) {
	// Fast path: this Clerk user has logged in before.
	ident, err := v.store.GetAuthIdentity(ctx, db.GetAuthIdentityParams{
		Provider: ProviderClerk,
		Subject:  clerkUserID,
	})
	if err == nil {
		user, err := v.store.GetUserByID(ctx, ident.UserID)
		if err != nil || !user.IsActive {
			return Identity{}, ErrInvalidCredentials
		}
		tenantID, err := v.resolveTenant(ctx, orgID, orgSlug)
		if err != nil {
			return Identity{}, err
		}
		return Identity{
			UserID:   user.ID,
			Username: user.Username,
			Email:    user.Email,
			IsAdmin:  user.IsAdmin,
			TenantID: tenantID,
		}, nil
	}

	// Slow path: first login — fetch profile from Clerk and create the Quill user.
	profile, err := v.fetchClerkUser(ctx, clerkUserID)
	if err != nil {
		v.logger.Error("clerk profile fetch failed", "clerkUserID", clerkUserID, "error", err)
		return Identity{}, ErrInvalidCredentials
	}

	id, err := v.provisionUser(ctx, clerkUserID, profile)
	if err != nil {
		v.logger.Error("user provisioning failed", "clerkUserID", clerkUserID, "error", err)
		return Identity{}, ErrInvalidCredentials
	}

	tenantID, err := v.resolveTenant(ctx, orgID, orgSlug)
	if err != nil {
		return Identity{}, err
	}
	id.TenantID = tenantID

	// Best-effort Forgejo provisioning — must not block the login response.
	if v.forgejo != nil && v.forgejo.Enabled() {
		go v.provisionForgejo(context.WithoutCancel(ctx), id)
	}

	return id, nil
}

// clerkUserProfile holds the fields Quill needs from the Clerk Users API.
type clerkUserProfile struct {
	ID                    string `json:"id"`
	Username              string `json:"username"`
	FirstName             string `json:"first_name"`
	LastName              string `json:"last_name"`
	PrimaryEmailAddr      string // resolved from EmailAddresses below
	PrimaryEmailAddressID string `json:"primary_email_address_id"`
	EmailAddresses        []struct {
		ID           string `json:"id"`
		EmailAddress string `json:"email_address"`
	} `json:"email_addresses"`
}

func (v *ClerkVerifier) fetchClerkUser(ctx context.Context, clerkUserID string) (clerkUserProfile, error) {
	if v.secretKey == "" {
		return clerkUserProfile{}, fmt.Errorf("QUILL_CLERK_SECRET_KEY not set")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"https://api.clerk.com/v1/users/"+clerkUserID, nil)
	if err != nil {
		return clerkUserProfile{}, err
	}
	req.Header.Set("Authorization", "Bearer "+v.secretKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return clerkUserProfile{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return clerkUserProfile{}, fmt.Errorf("clerk API returned %d for user %s", resp.StatusCode, clerkUserID)
	}

	var profile clerkUserProfile
	if err := json.NewDecoder(resp.Body).Decode(&profile); err != nil {
		return clerkUserProfile{}, err
	}

	for _, addr := range profile.EmailAddresses {
		if addr.ID == profile.PrimaryEmailAddressID {
			profile.PrimaryEmailAddr = addr.EmailAddress
			break
		}
	}
	return profile, nil
}

var invalidUsernameCharsRe = regexp.MustCompile(`[^a-zA-Z0-9_-]`)

func (v *ClerkVerifier) provisionUser(ctx context.Context, clerkUserID string, profile clerkUserProfile) (Identity, error) {
	username := deriveUsername(profile.Username, profile.PrimaryEmailAddr, clerkUserID)
	email := profile.PrimaryEmailAddr
	displayName := strings.TrimSpace(profile.FirstName + " " + profile.LastName)
	if displayName == "" {
		displayName = username
	}

	id, err := v.createUserWithIdentity(ctx, clerkUserID, username, email, displayName)
	if err == nil {
		return id, nil
	}
	if !isUniqueViolation(err) {
		return Identity{}, err
	}
	// Username collision: append the tail of the Clerk user ID and retry once.
	suffix := "-" + tailOf(clerkUserID, 8)
	username = username[:min(39-len(suffix), len(username))] + suffix
	return v.createUserWithIdentity(ctx, clerkUserID, username, email, displayName)
}

func (v *ClerkVerifier) createUserWithIdentity(ctx context.Context, clerkUserID, username, email, displayName string) (Identity, error) {
	var id Identity
	err := v.store.InTx(ctx, func(q *db.Queries) error {
		count, err := q.CountUsers(ctx)
		if err != nil {
			return err
		}
		user, err := q.CreateUser(ctx, db.CreateUserParams{
			Username:    username,
			Email:       email,
			DisplayName: displayName,
			IsAdmin:     count == 0,
			IsActive:    true,
		})
		if err != nil {
			return err
		}
		if _, err := q.CreateAuthIdentity(ctx, db.CreateAuthIdentityParams{
			UserID:     user.ID,
			Provider:   ProviderClerk,
			Subject:    clerkUserID,
			SecretHash: pgtype.Text{}, // Clerk manages credentials; no local hash.
		}); err != nil {
			return err
		}
		id = Identity{
			UserID:   user.ID,
			Username: user.Username,
			Email:    user.Email,
			IsAdmin:  user.IsAdmin,
		}
		return nil
	})
	return id, err
}

// resolveTenant returns the Quill tenant UUID for the given Clerk org. When
// orgID is empty (no active organisation in the JWT), the seeded default tenant
// is used so single-user setups keep working.
func (v *ClerkVerifier) resolveTenant(ctx context.Context, orgID, orgSlug string) (uuid.UUID, error) {
	if orgID == "" {
		tenant, err := v.store.GetTenantBySlug(ctx, "default")
		if err != nil {
			return uuid.UUID{}, fmt.Errorf("default tenant not found: %w", err)
		}
		return tenant.ID, nil
	}
	slug := orgSlug
	if slug == "" {
		slug = tailOf(orgID, 12)
	}
	tenant, err := v.store.GetOrCreateTenantByClerkOrg(ctx, orgID, slug)
	if err != nil {
		return uuid.UUID{}, fmt.Errorf("resolve org tenant for %s: %w", orgID, err)
	}
	return tenant.ID, nil
}

// provisionForgejo best-effort creates a Forgejo account mirroring the newly
// provisioned Quill user. Failures are logged and swallowed — the Forgejo link
// can be repaired later and must not prevent login.
func (v *ClerkVerifier) provisionForgejo(ctx context.Context, id Identity) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	fjUser, err := v.forgejo.CreateUser(ctx, forgejo.CreateUserOptions{
		Username:           id.Username,
		Email:              id.Email,
		Password:           randomSecret(),
		MustChangePassword: false,
	})
	if err != nil {
		existing, getErr := v.forgejo.GetUser(ctx, id.Username)
		if getErr != nil {
			v.logger.Error("forgejo provisioning failed for clerk user", "username", id.Username, "error", err)
			return
		}
		fjUser = existing
	}

	if _, err := v.store.SetUserForgejoLink(ctx, db.SetUserForgejoLinkParams{
		ID:              id.UserID,
		ForgejoUserID:   pgtype.Int8{Int64: fjUser.ID, Valid: true},
		ForgejoUsername: pgtype.Text{String: fjUser.Login, Valid: true},
	}); err != nil {
		v.logger.Error("failed to link forgejo user for clerk account", "username", id.Username, "error", err)
	}
}

func deriveUsername(clerkUsername, email, clerkUserID string) string {
	candidate := clerkUsername
	if candidate == "" && email != "" {
		if i := strings.IndexByte(email, '@'); i > 0 {
			candidate = email[:i]
		}
	}
	candidate = invalidUsernameCharsRe.ReplaceAllString(candidate, "-")
	if len(candidate) < 2 {
		candidate = "user-" + tailOf(clerkUserID, 8)
	}
	if len(candidate) > 39 {
		candidate = candidate[:39]
	}
	return candidate
}

func tailOf(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}
