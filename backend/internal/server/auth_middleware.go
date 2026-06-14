package server

import (
	"context"
	"net/http"
	"strings"

	"github.com/nielsuitterdijk22/quill/internal/auth"
	"github.com/nielsuitterdijk22/quill/internal/httpx"
)

// cookieToken is the name of the httpOnly cookie that may carry the access token
// (the frontend sets it; API clients may instead send Authorization: Bearer).
const cookieToken = "quill_token"

type ctxKey int

const identityKey ctxKey = iota

// requireAuth verifies the bearer token (or quill_token cookie) and attaches the
// resulting Identity to the request context. It responds 401 when missing or invalid.
func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := bearerToken(r)
		if token == "" {
			if c, err := r.Cookie(cookieToken); err == nil {
				token = c.Value
			}
		}
		if token == "" {
			httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
			return
		}

		id, err := s.auth.Tokens().Verify(token)
		if err != nil {
			httpx.Error(w, http.StatusUnauthorized, "unauthorized", "invalid or expired token")
			return
		}

		ctx := context.WithValue(r.Context(), identityKey, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// identityFrom returns the authenticated Identity stored by requireAuth.
func identityFrom(ctx context.Context) (auth.Identity, bool) {
	id, ok := ctx.Value(identityKey).(auth.Identity)
	return id, ok
}

func bearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if h == "" {
		return ""
	}
	const prefix = "Bearer "
	if len(h) > len(prefix) && strings.EqualFold(h[:len(prefix)], prefix) {
		return strings.TrimSpace(h[len(prefix):])
	}
	return ""
}
