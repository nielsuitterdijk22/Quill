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

// requireAuth verifies the bearer token (or quill_token cookie) and attaches
// the resulting Identity to the request context. It responds 401 when the
// token is missing or invalid.
//
// When Zitadel is configured it is tried first; the local HS256 JWT service is
// used as a fallback so the server can operate without Zitadel during development.
// The DB lookup that re-reads the user on every request (for deactivation and
// stale-admin detection) is embedded in ZitadelVerifier.Verify; for the local
// path it is performed here.
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

		var (
			id  auth.Identity
			err error
		)

		// External IdP path (Zitadel): verify the RS256 JWT and provision
		// user/tenant on first login.
		if s.externalAuthEnabled() {
			id, err = s.verifier.Verify(r.Context(), token)
			if err != nil {
				httpx.Error(w, http.StatusUnauthorized, "unauthorized", "invalid or expired token")
				return
			}
		} else {
			// Local path: HS256 JWT, then re-read the user from the DB to catch
			// deactivated accounts and stale admin claims.
			id, err = s.auth.Tokens().Verify(token)
			if err != nil {
				httpx.Error(w, http.StatusUnauthorized, "unauthorized", "invalid or expired token")
				return
			}
			user, err := s.store.GetUserByID(r.Context(), id.UserID)
			if err != nil || !user.IsActive {
				httpx.Error(w, http.StatusUnauthorized, "unauthorized", "account not found or inactive")
				return
			}
			id.IsAdmin = user.IsAdmin
		}

		ctx := context.WithValue(r.Context(), identityKey, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// requireAdmin rejects requests from non-admin users with 403. Must run after requireAuth.
func (s *Server) requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, ok := identityFrom(r.Context())
		if !ok || !id.IsAdmin {
			httpx.Error(w, http.StatusForbidden, "forbidden", "admin access required")
			return
		}
		next.ServeHTTP(w, r)
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

// publicRepoReadPath reports whether a request may be served without
// authentication: the code-browsing reads of a repository (GH-style public repo
// browsing). The platform layer enforces the real rule — a public repo is
// world-readable, everything else answers NotFound/Forbidden — so this only
// decides whether the request is *eligible* for anonymous handling. The
// patterns keep write operations (POST/PATCH/DELETE, pulls, issues, policies,
// pipelines, secrets, settings) strictly behind requireAuth.
func publicRepoReadPath(r *http.Request) bool {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		return false
	}
	// /api/v1/projects/{project}/repos/{repo}(/...)? — the repos subroute.
	path := r.URL.Path
	const prefix = "/projects/"
	const reposSegment = "/repos/"
	i := strings.Index(path, reposSegment)
	if i < 0 || !strings.HasPrefix(path, prefix) {
		return false
	}
	rest := path[i+len(reposSegment):]
	// rest is "{repo}" or "{repo}/{...}": repo slug then optional subpath.
	var sub string
	if j := strings.Index(rest, "/"); j >= 0 {
		sub = rest[j+1:]
		rest = rest[:j]
	}
	if rest == "" {
		return false
	}
	if sub == "" {
		return r.Method == http.MethodGet // GET / repos/{repo} — repo metadata
	}
	switch {
	case sub == "branches" || sub == "contents" || sub == "commits" || sub == "markup":
		return true
	case strings.HasPrefix(sub, "commits/"), strings.HasPrefix(sub, "contents/"):
		return true
	}
	return false
}

// requireAuthOrPublicRead is requireAuth relaxed for anonymous code browsing:
// requests to the public read paths may pass without a token and get an
// anonymous identity (zero-value Actor), which the platform authorizes only
// for public repositories. Everything else is exactly requireAuth.
func (s *Server) requireAuthOrPublicRead(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := bearerToken(r)
		if token == "" {
			if c, err := r.Cookie(cookieToken); err == nil {
				token = c.Value
			}
		}
		if token != "" || !publicRepoReadPath(r) {
			s.requireAuth(next).ServeHTTP(w, r)
			return
		}
		// Anonymous read on a repo path: let the platform decide visibility.
		ctx := context.WithValue(r.Context(), identityKey, auth.Identity{})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
