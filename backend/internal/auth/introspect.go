package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/google/uuid"

	"github.com/nielsuitterdijk22/quill/internal/store/db"
)

// PATIntrospector is the optional TokenVerifier extension a provider can
// implement to validate opaque personal access tokens (git-over-HTTPS uses it
// as the secondary credential; Quill-issued git tokens are the primary).
type PATIntrospector interface {
	Introspect(ctx context.Context, token string) (Identity, error)
}

// Introspect validates a Zitadel PAT via the introspection endpoint. The
// management token authorizes the call (a service-user PAT with introspect
// scope); when none is configured introspection is disabled — git stays up
// through Quill-issued tokens regardless of IdP state (the scale-to-zero
// property: no per-push round-trip to a cold IdP).
func (v *ZitadelVerifier) Introspect(ctx context.Context, token string) (Identity, error) {
	if v.mgmtToken == "" {
		return Identity{}, fmt.Errorf("%w: personal token introspection is not configured", ErrInvalidCredentials)
	}
	form := url.Values{"token": {token}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimSuffix(v.issuer, "/")+"/oauth/v2/introspect",
		strings.NewReader(form.Encode()))
	if err != nil {
		return Identity{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization", "Bearer "+v.mgmtToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return Identity{}, fmt.Errorf("zitadel introspection: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return Identity{}, ErrInvalidCredentials
	}
	var out struct {
		Active   bool   `json:"active"`
		Sub      string `json:"sub"`
		Username string `json:"username"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return Identity{}, fmt.Errorf("zitadel introspection decode: %w", err)
	}
	if !out.Active || out.Sub == "" {
		return Identity{}, ErrInvalidCredentials
	}
	// Map exactly like a verified login: an unknown subject is NOT
	// provisioned from a git push — the user must have signed in once.
	ident, err := v.store.GetAuthIdentity(ctx, db.GetAuthIdentityParams{
		Provider: ProviderZitadel,
		Subject:  out.Sub,
	})
	if err != nil {
		return Identity{}, ErrInvalidCredentials
	}
	user, err := v.store.GetUserByID(ctx, ident.UserID)
	if err != nil || !user.IsActive {
		return Identity{}, ErrInvalidCredentials
	}
	var tenantID uuid.UUID
	if user.TenantID.Valid {
		tenantID = user.TenantID.UUID
	}
	return Identity{
		UserID:   user.ID,
		Username: user.Username,
		Email:    user.Email,
		IsAdmin:  user.IsAdmin,
		TenantID: tenantID,
	}, nil
}
