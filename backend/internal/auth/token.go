package auth

import (
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/nielsuitterdijk22/quill/internal/config"
)

// devSecret is used only when no JWT secret is configured (development). Load()
// requires QUILL_JWT_SECRET in production, so this fallback never applies there.
const devSecret = "quill-dev-insecure-secret-change-me"

// Claims are the JWT claims Quill issues. The subject is the user id; the rest
// mirror Identity so middleware can build an Identity without a database hit.
type Claims struct {
	jwt.RegisteredClaims
	Username string `json:"username"`
	Email    string `json:"email"`
	IsAdmin  bool   `json:"isAdmin"`
}

// TokenService issues and verifies Quill access tokens (HS256 JWTs).
type TokenService struct {
	secret []byte
	issuer string
	ttl    time.Duration
}

// NewTokenService builds a TokenService from JWT config, falling back to an
// insecure development secret when none is set.
func NewTokenService(cfg config.JWTConfig) *TokenService {
	secret := cfg.Secret
	if secret == "" {
		secret = devSecret
	}
	ttl := cfg.TTL
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	return &TokenService{secret: []byte(secret), issuer: cfg.Issuer, ttl: ttl}
}

// TTL returns the configured token lifetime (used to align cookie expiry).
func (t *TokenService) TTL() time.Duration { return t.ttl }

// Issue mints a signed token for id and returns it with its expiry time.
func (t *TokenService) Issue(id Identity) (string, time.Time, error) {
	now := time.Now()
	exp := now.Add(t.ttl)
	claims := Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    t.issuer,
			Subject:   id.UserID.String(),
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(exp),
		},
		Username: id.Username,
		Email:    id.Email,
		IsAdmin:  id.IsAdmin,
	}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(t.secret)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("sign token: %w", err)
	}
	return signed, exp, nil
}

// Verify parses and validates a token, returning the embedded Identity.
func (t *TokenService) Verify(token string) (Identity, error) {
	claims := &Claims{}
	parsed, err := jwt.ParseWithClaims(token, claims, func(tok *jwt.Token) (any, error) {
		if _, ok := tok.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", tok.Header["alg"])
		}
		return t.secret, nil
	}, jwt.WithIssuer(t.issuer), jwt.WithValidMethods([]string{"HS256"}))
	if err != nil || !parsed.Valid {
		return Identity{}, ErrInvalidCredentials
	}

	uid, err := uuid.Parse(claims.Subject)
	if err != nil {
		return Identity{}, ErrInvalidCredentials
	}
	return Identity{
		UserID:   uid,
		Username: claims.Username,
		Email:    claims.Email,
		IsAdmin:  claims.IsAdmin,
	}, nil
}
