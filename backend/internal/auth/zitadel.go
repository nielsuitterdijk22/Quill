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

// Zitadel exposes a user's organisation as these reserved claims on the issued
// token. The org id drives Quill's tenant resolution (one tenant per org);
// primary_domain is a human-readable slug fallback.
const (
	zitadelOrgIDClaim     = "urn:zitadel:iam:user:resourceowner:id"
	zitadelOrgDomainClaim = "urn:zitadel:iam:user:resourceowner:primary_domain"
)

// ZitadelVerifier verifies Zitadel-issued RS256 JWTs against the instance JWKS
// and provisions Quill users and tenants on first login. It reads the user
// profile from the standard OIDC userinfo endpoint and maps Zitadel's org claims
// to Quill tenants. Implements TokenVerifier.
type ZitadelVerifier struct {
	store     *store.Store
	forgejo   *forgejo.Client
	logger    *slog.Logger
	issuer    string
	jwksURL   string
	userinfo  string
	mgmtToken string
	mu        sync.RWMutex
	keySet    jwk.Set
}

// NewZitadelVerifier builds a verifier from configuration. Call Start to fetch
// the initial JWKS and begin background refresh.
func NewZitadelVerifier(cfg config.ZitadelConfig, st *store.Store, logger *slog.Logger) *ZitadelVerifier {
	issuer := strings.TrimSuffix(cfg.Issuer, "/")
	return &ZitadelVerifier{
		store:     st,
		logger:    logger,
		issuer:    issuer,
		jwksURL:   issuer + "/oauth/v2/keys",
		userinfo:  issuer + "/oidc/v1/userinfo",
		mgmtToken: cfg.ManagementToken,
	}
}

// WithForgejo enables best-effort Forgejo user provisioning on first login.
func (v *ZitadelVerifier) WithForgejo(fj *forgejo.Client) *ZitadelVerifier {
	v.forgejo = fj
	return v
}

// Enabled reports whether Zitadel authentication is configured.
func (v *ZitadelVerifier) Enabled() bool { return v.issuer != "" }

// Provider returns the auth_identities.provider key for Zitadel.
func (v *ZitadelVerifier) Provider() string { return ProviderZitadel }

// Start fetches the initial JWKS synchronously and refreshes it every 15 minutes.
func (v *ZitadelVerifier) Start(ctx context.Context) {
	if !v.Enabled() {
		return
	}
	if err := v.refresh(ctx); err != nil {
		v.logger.Warn("initial Zitadel JWKS fetch failed — auth will fail until retry", "error", err)
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
					v.logger.Warn("Zitadel JWKS refresh failed", "error", err)
				}
			}
		}
	}()
}

func (v *ZitadelVerifier) refresh(ctx context.Context) error {
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

// Verify validates a Zitadel access-token JWT and returns the resolved identity.
func (v *ZitadelVerifier) Verify(ctx context.Context, token string) (Identity, error) {
	v.mu.RLock()
	ks := v.keySet
	v.mu.RUnlock()
	if ks == nil {
		if err := v.refresh(ctx); err != nil {
			v.logger.Warn("zitadel verify: JWKS unavailable", "jwksURL", v.jwksURL, "error", err)
			return Identity{}, ErrInvalidCredentials
		}
		v.mu.RLock()
		ks = v.keySet
		v.mu.RUnlock()
	}

	tok, err := jwt.Parse(
		[]byte(token),
		jwt.WithKeySet(ks, jws.WithInferAlgorithmFromKey(true)),
		jwt.WithIssuer(v.issuer),
		jwt.WithValidate(true),
		jwt.WithAcceptableSkew(30*time.Second),
	)
	if err != nil {
		// Log the concrete reason: signature mismatch, issuer mismatch, expiry, or
		// a non-JWT (opaque) token. Include the token's own iss/aud when parseable
		// without verification so an issuer mismatch is obvious at a glance.
		v.logger.Warn("zitadel verify: token rejected",
			"error", err,
			"expectedIssuer", v.issuer,
			"tokenIssuer", unverifiedClaim(token, "iss"),
			"tokenAud", unverifiedClaim(token, "aud"),
		)
		return Identity{}, ErrInvalidCredentials
	}

	sub, ok := tok.Subject()
	if !ok || sub == "" {
		v.logger.Warn("zitadel verify: token missing subject")
		return Identity{}, ErrInvalidCredentials
	}

	var orgID, orgDomain string
	_ = tok.Get(zitadelOrgIDClaim, &orgID)
	_ = tok.Get(zitadelOrgDomainClaim, &orgDomain)

	return v.resolveIdentity(ctx, token, sub, orgID, orgDomain)
}

// unverifiedClaim decodes a JWT WITHOUT verifying its signature and returns the
// named string claim, or "" if the token isn't a JWT or lacks the claim. For
// diagnostics only — never trust the result for authorization. "aud" (a []string
// or string) is rendered best-effort.
func unverifiedClaim(token, claim string) string {
	tok, err := jwt.Parse([]byte(token), jwt.WithVerify(false), jwt.WithValidate(false))
	if err != nil {
		return "" // not a JWT (e.g. opaque token) — the caller's error already says so.
	}
	if claim == "iss" {
		if iss, ok := tok.Issuer(); ok {
			return iss
		}
		return ""
	}
	var v any
	if err := tok.Get(claim, &v); err != nil {
		return ""
	}
	return fmt.Sprintf("%v", v)
}

// resolveIdentity looks up or provisions the Quill user and tenant for a Zitadel
// (userID, orgID) pair. rawToken is the bearer used to read the OIDC userinfo
// profile on first login.
func (v *ZitadelVerifier) resolveIdentity(ctx context.Context, rawToken, userID, orgID, orgDomain string) (Identity, error) {
	// Fast path: this Zitadel user has logged in before.
	ident, err := v.store.GetAuthIdentity(ctx, db.GetAuthIdentityParams{
		Provider: ProviderZitadel,
		Subject:  userID,
	})
	if err == nil {
		user, err := v.store.GetUserByID(ctx, ident.UserID)
		if err != nil || !user.IsActive {
			return Identity{}, ErrInvalidCredentials
		}
		tenantID, err := v.resolveTenant(ctx, orgID, orgDomain)
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

	// Slow path: first login — read the profile from userinfo and create the user.
	profile, err := v.fetchUserInfo(ctx, rawToken)
	if err != nil {
		v.logger.Error("zitadel userinfo fetch failed", "userID", userID, "error", err)
		return Identity{}, ErrInvalidCredentials
	}

	id, err := v.provisionUser(ctx, userID, profile)
	if err != nil {
		v.logger.Error("user provisioning failed", "userID", userID, "error", err)
		return Identity{}, ErrInvalidCredentials
	}

	tenantID, err := v.resolveTenant(ctx, orgID, orgDomain)
	if err != nil {
		return Identity{}, err
	}
	id.TenantID = tenantID

	// Best-effort Forgejo provisioning, off the login path.
	if v.forgejo != nil && v.forgejo.Enabled() {
		go v.provisionForgejo(context.WithoutCancel(ctx), id)
	}
	return id, nil
}

// zitadelUserInfo holds the OIDC userinfo fields Quill needs.
type zitadelUserInfo struct {
	Sub               string `json:"sub"`
	Email             string `json:"email"`
	PreferredUsername string `json:"preferred_username"`
	Name              string `json:"name"`
	GivenName         string `json:"given_name"`
	FamilyName        string `json:"family_name"`
}

func (v *ZitadelVerifier) fetchUserInfo(ctx context.Context, rawToken string) (zitadelUserInfo, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, v.userinfo, nil)
	if err != nil {
		return zitadelUserInfo{}, err
	}
	req.Header.Set("Authorization", "Bearer "+rawToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return zitadelUserInfo{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return zitadelUserInfo{}, fmt.Errorf("zitadel userinfo returned %d", resp.StatusCode)
	}
	var info zitadelUserInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return zitadelUserInfo{}, err
	}
	return info, nil
}

func (v *ZitadelVerifier) provisionUser(ctx context.Context, userID string, info zitadelUserInfo) (Identity, error) {
	// preferred_username is often a login name like "user@domain"; take the local
	// part as the username seed so derived handles stay clean.
	seed := info.PreferredUsername
	if i := strings.IndexByte(seed, '@'); i > 0 {
		seed = seed[:i]
	}
	username := deriveUsername(seed, info.Email, userID)
	displayName := strings.TrimSpace(info.Name)
	if displayName == "" {
		displayName = strings.TrimSpace(info.GivenName + " " + info.FamilyName)
	}
	if displayName == "" {
		displayName = username
	}

	id, err := v.createUserWithIdentity(ctx, userID, username, info.Email, displayName)
	if err == nil {
		return id, nil
	}
	if !isUniqueViolation(err) {
		return Identity{}, err
	}
	// Username collision: append the tail of the Zitadel user ID and retry once.
	suffix := "-" + tailOf(userID, 8)
	username = username[:min(39-len(suffix), len(username))] + suffix
	return v.createUserWithIdentity(ctx, userID, username, info.Email, displayName)
}

func (v *ZitadelVerifier) createUserWithIdentity(ctx context.Context, userID, username, email, displayName string) (Identity, error) {
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
			Provider:   ProviderZitadel,
			Subject:    userID,
			SecretHash: pgtype.Text{}, // Zitadel manages credentials; no local hash.
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

// resolveTenant maps a Zitadel org to a Quill tenant (one tenant per org). When
// orgID is empty the seeded default tenant is used so single-user setups work.
// The org->tenant mapping uses the tenants.external_org_id column.
func (v *ZitadelVerifier) resolveTenant(ctx context.Context, orgID, orgDomain string) (uuid.UUID, error) {
	if orgID == "" {
		tenant, err := v.store.GetTenantBySlug(ctx, "default")
		if err != nil {
			return uuid.UUID{}, fmt.Errorf("default tenant not found: %w", err)
		}
		return tenant.ID, nil
	}
	slug := slugifyOrgDomain(orgDomain)
	if slug == "" {
		slug = tailOf(orgID, 12)
	}
	tenant, err := v.store.GetOrCreateTenantByExternalOrg(ctx, orgID, slug)
	if err != nil {
		return uuid.UUID{}, fmt.Errorf("resolve org tenant for %s: %w", orgID, err)
	}
	return tenant.ID, nil
}

var invalidUsernameCharsRe = regexp.MustCompile(`[^a-zA-Z0-9_-]`)

// reservedUsernames mirrors the frontend top-level routes so derived usernames
// never shadow an app page. Kept in sync with platform.reservedSlugs.
var reservedUsernames = map[string]bool{
	"new": true, "edit": true, "settings": true, "api": true,
	"projects": true, "repositories": true, "pulls": true, "pipelines": true,
	"admin": true, "sign-in": true, "sign-up": true, "login": true, "register": true,
}

// deriveUsername builds a clean Quill username from the IdP-provided handle,
// falling back to the email local-part and finally the tail of the subject ID.
func deriveUsername(handle, email, subject string) string {
	candidate := handle
	if candidate == "" && email != "" {
		if i := strings.IndexByte(email, '@'); i > 0 {
			candidate = email[:i]
		}
	}
	candidate = invalidUsernameCharsRe.ReplaceAllString(candidate, "-")
	if len(candidate) < 2 || reservedUsernames[strings.ToLower(candidate)] {
		candidate = "user-" + tailOf(subject, 8)
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

// slugifyOrgDomain turns a Zitadel primary domain (e.g. "acme.quill.local") into
// a tenant slug ("acme") by taking the leading label.
func slugifyOrgDomain(domain string) string {
	domain = strings.TrimSpace(strings.ToLower(domain))
	if domain == "" {
		return ""
	}
	if i := strings.IndexByte(domain, '.'); i > 0 {
		return domain[:i]
	}
	return domain
}

// provisionForgejo best-effort mirrors the Quill user into Forgejo. Failures are
// logged and swallowed — they must never block login.
func (v *ZitadelVerifier) provisionForgejo(ctx context.Context, id Identity) {
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
			v.logger.Error("forgejo provisioning failed for zitadel user", "username", id.Username, "error", err)
			return
		}
		fjUser = existing
	}
	if _, err := v.store.SetUserForgejoLink(ctx, db.SetUserForgejoLinkParams{
		ID:              id.UserID,
		ForgejoUserID:   pgtype.Int8{Int64: fjUser.ID, Valid: true},
		ForgejoUsername: pgtype.Text{String: fjUser.Login, Valid: true},
	}); err != nil {
		v.logger.Error("failed to link forgejo user for zitadel account", "username", id.Username, "error", err)
	}
}

// DeleteUser removes the Zitadel-side user via the Management API so a deleted
// account's session can't re-provision a fresh Quill user on the next request. A
// 404 is treated as success. Requires ZITADEL_MANAGEMENT_TOKEN; when it is
// unset the call is skipped with a warning (the Quill account is already gone).
func (v *ZitadelVerifier) DeleteUser(ctx context.Context, subject string) error {
	if v.mgmtToken == "" {
		return fmt.Errorf("ZITADEL_MANAGEMENT_TOKEN not set")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete,
		v.issuer+"/management/v1/users/"+subject, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+v.mgmtToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNotFound {
		return fmt.Errorf("zitadel delete user returned %d for %s", resp.StatusCode, subject)
	}
	return nil
}
