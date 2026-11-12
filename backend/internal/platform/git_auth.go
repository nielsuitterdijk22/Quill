package platform

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/nielsuitterdijk22/quill/internal/auth"
	"github.com/nielsuitterdijk22/quill/internal/store"
	"github.com/nielsuitterdijk22/quill/internal/store/db"
)

// Git endpoint auth/tenancy (serverless epic, PR B6). Two credentials over
// HTTPS Basic: Quill-issued git tokens (primary — validated locally by hash
// lookup, no IdP round-trip, revocable) and Zitadel PATs (secondary —
// introspected through the auth package, mirroring a verified login).
// Anonymous requests are allowed and confined to public-repo reads; pushes
// always require an authenticated member with a write-level project role.

// ErrGitUnauthorized means credentials were presented but invalid: the git
// surface must answer 401 (never 403 — Basic auth clients retry on 401).
var ErrGitUnauthorized = errors.New("invalid git credentials")

// nativeTokenPrefix marks Quill-issued git tokens.
const nativeTokenPrefix = "qgt_"

// GitActor is the authenticated identity behind one git request (or
// anonymous when Authenticated is false).
type GitActor struct {
	UserID        uuid.UUID
	Username      string
	IsAdmin       bool
	Authenticated bool
}

// GitPATIntrospector is the seam the server wires from the auth package so
// platform never talks to an auth provider directly (AuthProvider rule).
type GitPATIntrospector interface {
	// Introspect maps an opaque provider PAT to the Quill identity. It is
	// shape-compatible with auth.PATIntrospector, so the verifier itself can be
	// wired without an adapter.
	Introspect(ctx context.Context, token string) (auth.Identity, error)
}

// WithGitPATIntrospector installs the secondary credential path.
func (s *Service) WithGitPATIntrospector(in GitPATIntrospector) *Service {
	s.gitPAT = in
	return s
}

// AuthenticateGitRequest resolves the identity behind a git HTTP request.
// Absent credentials are anonymous (not an error); INVALID credentials are
// ErrGitUnauthorized so the handler can 401 with a Basic challenge.
func (s *Service) AuthenticateGitRequest(ctx context.Context, r *http.Request) (GitActor, error) {
	username, password, ok := r.BasicAuth()
	if !ok || (username == "" && password == "") {
		return GitActor{UserID: uuid.Nil}, nil
	}
	if password == "" {
		return GitActor{}, ErrGitUnauthorized
	}
	// Primary: Quill-issued git token (username is cosmetic).
	if strings.HasPrefix(password, nativeTokenPrefix) {
		sum := sha256.Sum256([]byte(password))
		_, userID, err := s.store.GitTokenByHash(ctx, hex.EncodeToString(sum[:]))
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return GitActor{}, ErrGitUnauthorized
			}
			return GitActor{}, err
		}
		user, err := s.store.GetUserByID(ctx, userID)
		if err != nil || !user.IsActive {
			return GitActor{}, ErrGitUnauthorized
		}
		return GitActor{UserID: user.ID, Username: user.Username, IsAdmin: user.IsAdmin, Authenticated: true}, nil
	}
	// Secondary: provider PAT introspection (configured providers only).
	if s.gitPAT != nil {
		ident, err := s.gitPAT.Introspect(ctx, password)
		if err == nil {
			return GitActor{UserID: ident.UserID, Username: ident.Username, IsAdmin: ident.IsAdmin, Authenticated: true}, nil
		}
		// A provider-level rejection is a 401 for the git surface — the
		// client gets a clean prompt, not a 500 leaking provider details.
		return GitActor{}, ErrGitUnauthorized
	}
	return GitActor{}, ErrGitUnauthorized
}

// ResolveRepoForGitAccess is the git endpoint's scoped resolver (B6): the
// anonymous world may read public repos; members read everything they can
// browse; pushes require an authenticated actor with a write-level project
// role. Private repos are NotFound to non-members (existence never leaks).
func (s *Service) ResolveRepoForGitAccess(ctx context.Context, ga GitActor, projectSlug, repoSlug string, wantPush bool) (store.GitRepoRef, error) {
	ref, err := s.store.FindRepoForGit(ctx, projectSlug, repoSlug)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return store.GitRepoRef{}, ErrNotFound
		}
		return store.GitRepoRef{}, err
	}
	actor := Actor{UserID: ga.UserID, IsAdmin: ga.IsAdmin}
	if wantPush {
		if !ga.Authenticated {
			return store.GitRepoRef{}, ErrForbidden
		}
		role, err := s.effectiveProjectRole(ctx, actor, ref.ProjectID)
		if err != nil {
			return store.GitRepoRef{}, err
		}
		if role == "" || role == "read" {
			return store.GitRepoRef{}, ErrForbidden
		}
		return ref, nil
	}
	role, _ := s.effectiveProjectRole(ctx, actor, ref.ProjectID)
	if role != "" {
		return ref, nil
	}
	repo, err := s.store.GetRepositoryByID(ctx, ref.RepoID)
	if err != nil {
		return store.GitRepoRef{}, err
	}
	if repo.Visibility == VisibilityPublic {
		return ref, nil
	}
	// Not visible: 404 for anonymous callers (no existence leak), forbidden
	// otherwise — the browse convention.
	if ga.UserID == uuid.Nil {
		return store.GitRepoRef{}, ErrNotFound
	}
	return store.GitRepoRef{}, ErrForbidden
}

// LogGitPush records a successful push: tenant-scoped audit entry + the
// daily push meter. Best-effort — a meter or audit hiccup must not fail the
// push that already committed.
func (s *Service) LogGitPush(ctx context.Context, tenantID uuid.UUID, actorID uuid.NullUUID, actorName, projectSlug, repoSlug, ip string) {
	meta, _ := json.Marshal(map[string]string{"project": projectSlug, "repo": repoSlug})
	if _, err := s.store.InsertAuditLog(ctx, db.InsertAuditLogParams{
		ActorUserID:   actorID,
		Action:        "git.push",
		TargetType:    "repository",
		TargetID:      repoSlug,
		Metadata:      meta,
		IPAddress:     ip,
		ActorUsername: actorName,
		TenantID:      uuid.NullUUID{UUID: tenantID, Valid: tenantID != uuid.Nil},
	}); err != nil {
		s.logger.Warn("git push audit write failed", "error", err)
	}
	if tenantID != uuid.Nil {
		if err := s.store.BumpPushMeter(ctx, tenantID); err != nil {
			s.logger.Warn("git push meter bump failed", "error", err)
		}
	}
}

// MintGitToken creates a Quill-native git token (own-engine mode): the
// secret is shown exactly once, only its SHA-256 is stored. The Forgejo-backed
// flow in git.go stays for Forgejo-hosted repos until Phase D.
func (s *Service) MintGitToken(ctx context.Context, actor Actor, name string) (GitCredential, error) {
	user, err := s.store.GetUserByID(ctx, actor.UserID)
	if err != nil {
		return GitCredential{}, fmt.Errorf("load user: %w", err)
	}
	label := strings.TrimSpace(name)
	if label == "" {
		label = "git access token"
	}
	if len(label) > 100 {
		label = label[:100]
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return GitCredential{}, err
	}
	secret := nativeTokenPrefix + base64.RawURLEncoding.EncodeToString(raw)
	sum := sha256.Sum256([]byte(secret))
	tokenID, err := s.store.CreateNativeGitToken(ctx, actor.UserID, label, hex.EncodeToString(sum[:]))
	if err != nil {
		return GitCredential{}, err
	}
	return GitCredential{ID: tokenID, Username: user.Username, Token: secret}, nil
}
