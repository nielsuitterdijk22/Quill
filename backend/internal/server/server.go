// Package server wires the HTTP router, middleware, and route handlers.
package server

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/google/uuid"

	"github.com/nielsuitterdijk22/quill/internal/auth"
	"github.com/nielsuitterdijk22/quill/internal/config"
	"github.com/nielsuitterdijk22/quill/internal/creem"
	"github.com/nielsuitterdijk22/quill/internal/forgejo"
	"github.com/nielsuitterdijk22/quill/internal/gitengine"
	"github.com/nielsuitterdijk22/quill/internal/objectstore"
	"github.com/nielsuitterdijk22/quill/internal/pipeline"
	"github.com/nielsuitterdijk22/quill/internal/platform"
	"github.com/nielsuitterdijk22/quill/internal/projectsync"
	"github.com/nielsuitterdijk22/quill/internal/secretbox"
	"github.com/nielsuitterdijk22/quill/internal/store"
	"github.com/nielsuitterdijk22/quill/internal/workitemrefs"
	"github.com/nielsuitterdijk22/quill/internal/zitadel"
)

// Version is the API version reported by the /api/v1/meta endpoint.
const Version = "0.1.0"

// Server is the root HTTP handler for the Quill backend.
type Server struct {
	cfg          *config.Config
	logger       *slog.Logger
	store        *store.Store
	auth         *auth.Service
	verifier     auth.TokenVerifier
	forgejo      *forgejo.Client
	platform     *platform.Service
	projectSync  *projectsync.Dispatcher
	workItemRefs *workitemrefs.Dispatcher
	router       chi.Router
	// handler is router behind the git-host rewrite (git.<domain> → /git).
	handler     http.Handler
	markupCache *markupCache
	// gitCache is the git engine's hydration cache; nil unless the own
	// engine is on (then the /git smart-HTTP routes are mounted).
	gitCache *gitengine.RepoCache
	// gitEngine is the bucket-side engine behind gitCache (receive path).
	gitEngine *gitengine.Engine
}

// gitActorKey carries the authenticated git surface actor through the request
// context (set by the /git auth middleware).
type gitActorKey struct{}

// externalAuthEnabled reports whether an external IdP (Zitadel) is configured
// and ready. When false, Quill falls back to local username/password auth
// (register/login/password routes are registered).
func (s *Server) externalAuthEnabled() bool {
	return s.verifier != nil && s.verifier.Enabled()
}

// New constructs a Server with middleware and routes configured. store may be
// nil in tests that only exercise handlers which don't touch the database.
func New(cfg *config.Config, logger *slog.Logger, st *store.Store) *Server {
	fj := forgejo.New(cfg.Forgejo)
	platformSvc := platform.NewService(st, fj, logger)
	if cfg.Pipeline.DispatchURL != "" {
		platformSvc.WithRunner(pipeline.NewHTTPRunner(cfg.Pipeline.DispatchURL, cfg.Pipeline.DispatchSecret))
	}
	// Queue mode (serverless epic, PR A1): the trigger path persists runs and
	// enqueues; execution moves to workers claiming via the internal dispatch
	// endpoints. Off by default — production keeps the inline VM-dispatch path
	// until the broker/worker PRs land.
	if cfg.Pipeline.QueueMode {
		platformSvc.WithQueueMode()
		// PR A2: each enqueue wakes the broker so an ephemeral CI instance is
		// ensured without any polling component (the sweeper is the backstop).
		if cfg.Pipeline.BrokerURL != "" {
			platformSvc.WithBroker(cfg.Pipeline.BrokerURL, cfg.Pipeline.DispatchSecret)
		}
	}
	// Own git engine (serverless epic, PR B1): QUILL_GIT_ENGINE_MODE=own
	// switches repository provisioning to the bucket layout + Postgres
	// referee. Off by default — the Forgejo path stays until Phase D's
	// cutover, so deploys of this code are inert without the env set.
	// config.Load already validated the bucket credentials (fail fast).
	var gitCache *gitengine.RepoCache
	var gitEngine *gitengine.Engine
	if cfg.GitEngine.Own() {
		objStore := objectstore.New(objectstore.Config{
			Endpoint:  cfg.GitEngine.S3Endpoint,
			Region:    cfg.GitEngine.S3Region,
			Bucket:    cfg.GitEngine.S3Bucket,
			AccessKey: cfg.GitEngine.S3AccessKey,
			SecretKey: cfg.GitEngine.S3SecretKey,
		})
		engine := gitengine.New(objStore)
		platformSvc.WithGitEngine(engine)
		cacheDir := cfg.GitEngine.CacheDir
		if cacheDir == "" {
			cacheDir = filepath.Join(os.TempDir(), "quill-git")
		}
		gitEngine = engine
		gitCache = &gitengine.RepoCache{
			Root:   cacheDir,
			Engine: engine,
			Packs:  gitengine.NewBucketPackSource(objStore),
		}
		// B4: code browse for engine-backed repos reads from the hydrated
		// cache (server wiring mirrors the internal seams of the /git mount).
		platformSvc.WithGitCache(gitCache)
		// D2: CI jobs clone engine repos from the public git base with a
		// scoped run token signed by the dispatch secret (same trust group).
		platformSvc.WithGitPublicURL(cfg.GitEngine.PublicURL).
			WithGitRunTokenSecret(cfg.Pipeline.DispatchSecret)
		// SBOM per default-branch push: syft is baked into the image; a
		// missing binary only fails individual generations (status: failed).
		if !cfg.SBOMDisabled {
			platformSvc.WithSBOMGenerator(platform.SyftGenerator{Bin: cfg.SBOMSyftBin})
		}
	}
	// Install the configured pipeline-secret cipher. NewService defaults to the
	// insecure dev cipher; a set QUILL_SECRET_ENCRYPTION_KEY overrides it. Load()
	// requires the key in production, so the dev fallback only applies locally.
	if cipher, err := secretbox.NewFromBase64Key(cfg.SecretEncryptionKey); err == nil {
		platformSvc.WithCipher(cipher)
	} else if !errors.Is(err, secretbox.ErrKeyMissing) {
		logger.Warn("invalid QUILL_SECRET_ENCRYPTION_KEY; using development cipher", "error", err)
	}
	// Org provisioning + member invites through Zitadel's Management API and mail
	// service. Idle unless both the issuer and a management token are configured;
	// when absent, orgs are Quill-only and invites use shareable accept links.
	if cfg.Zitadel.Issuer != "" && cfg.Zitadel.ManagementToken != "" {
		platformSvc.WithOrgProvisioner(zitadel.NewClient(cfg.Zitadel.Issuer, cfg.Zitadel.ManagementToken))
	}
	// Creem merchant-of-record integration for plan upgrades. With empty
	// credentials (the default) upgrade requests answer 501 not_configured
	// and the webhook endpoint refuses events until a secret is set.
	if cfg.Creem.APIKey != "" || cfg.Creem.WebhookSecret != "" {
		platformSvc.WithCreem(creem.New(cfg.Creem))
	}

	// The external IdP (Zitadel) verifies a bearer JWT, provisions the user on
	// first login, and maps the org claim to a Quill tenant. Personal project
	// creation happens during onboarding, so this path does not create one here.
	var verifier auth.TokenVerifier
	if cfg.Zitadel.Issuer != "" {
		verifier = auth.NewZitadelVerifier(cfg.Zitadel, st, logger).WithForgejo(fj)
	}

	// Git endpoint secondary credential (serverless epic, B6): Zitadel PATs
	// introspect through the verifier itself when a management token is
	// configured; the verifier self-guards otherwise.
	if zv, ok := verifier.(*auth.ZitadelVerifier); ok {
		platformSvc.WithGitPATIntrospector(zv)
	}

	// Project-mirror dispatcher: pushes project create/delete events to Tempo.
	// Idle unless QUILL_TEMPO_SYNC_URL is set. The token is acquired through a
	// TokenSource so the Zitadel machine token (PR 8.1) can replace the static one.
	var projectSync *projectsync.Dispatcher
	// Work-item ref dispatcher: pushes commit/PR cross-links scanned from Forgejo
	// webhooks to Tempo's refs endpoint. Idle unless QUILL_TEMPO_SYNC_REFS_URL is
	// set; shares the TempoSync token seam with the project-mirror dispatcher.
	var workItemRefs *workitemrefs.Dispatcher
	if st != nil {
		// Select once and share: when the QUILL_TEMPO_SYNC_ZITADEL_* machine-user
		// credentials are set, both dispatchers authenticate to Tempo with the same
		// cached Zitadel client-credentials token; otherwise both fall back to the
		// static QUILL_TEMPO_SYNC_TOKEN.
		tempoTokens := projectsync.SelectTempoTokenSource(cfg.TempoSync)
		projectSync = projectsync.NewDispatcher(
			projectsync.Config{URL: cfg.TempoSync.URL},
			st,
			tempoTokens,
			logger,
		)
		workItemRefs = workitemrefs.NewDispatcher(
			workitemrefs.Config{URL: cfg.TempoSync.RefsURL},
			st,
			tempoTokens,
			logger,
		)
		workItemRefs = workitemrefs.NewDispatcher(
			workitemrefs.Config{URL: cfg.TempoSync.RefsURL},
			st,
			tempoTokens,
			logger,
		)
	}

	s := &Server{
		cfg:          cfg,
		logger:       logger,
		store:        st,
		auth:         auth.NewService(st, auth.NewLocalProvider(st), auth.NewTokenService(cfg.JWT)).WithForgejo(fj, logger),
		verifier:     verifier,
		forgejo:      fj,
		platform:     platformSvc,
		projectSync:  projectSync,
		workItemRefs: workItemRefs,
		router:       chi.NewRouter(),
		markupCache:  newMarkupCache(),
		gitCache:     gitCache,
		gitEngine:    gitEngine,
	}
	s.setupMiddleware()
	s.setupRoutes()
	s.handler = gitHostRewrite(cfg.GitEngine.Host, s.router)
	return s
}

// gitHostRewrite serves the dedicated git host (git.<domain>, pointed at the
// api container in the D2 cutover) from the /git mount, so clone URLs keep
// the familiar https://git.<domain>/<project>/<repo>.git shape. Other hosts
// pass through untouched; an empty host disables the rewrite.
func gitHostRewrite(gitHost string, next http.Handler) http.Handler {
	gitHost = strings.ToLower(strings.TrimSpace(gitHost))
	if gitHost == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := strings.ToLower(r.Host)
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		if host == gitHost && r.URL.Path != "/git" && !strings.HasPrefix(r.URL.Path, "/git/") {
			r2 := r.Clone(r.Context())
			r2.URL.Path = "/git" + r.URL.Path
			if r.URL.RawPath != "" {
				r2.URL.RawPath = "/git" + r.URL.RawPath
			}
			next.ServeHTTP(w, r2)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// gitPostReceive backs the engine's OnRefsUpdated seam: CI triggers (and
// SBOM generation on the default branch) for the refs a push moved. Runs
// detached — the push already committed and must not wait on CI.
func (s *Server) gitPostReceive(rcx gitengine.ReceiveContext, _ string, cmds []gitengine.PushCommand) {
	repoID, err := uuid.Parse(rcx.ID)
	if err != nil {
		return
	}
	updates := make([]platform.EngineRefUpdate, 0, len(cmds))
	for _, c := range cmds {
		updates = append(updates, platform.EngineRefUpdate{Ref: c.Ref, Old: c.Old, New: c.New})
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		if _, err := s.platform.HandleEnginePush(ctx, repoID, updates); err != nil {
			s.logger.Warn("engine post-receive failed", "repo", rcx.ProjectSlug+"/"+rcx.RepoSlug, "error", err)
		}
	}()
}

// StartAuth begins background JWKS refresh for the configured external IdP's
// JWT verification. It must be called once the server's context is available
// (i.e. after New). No-op when only local auth is configured.
func (s *Server) StartAuth(ctx context.Context) {
	if s.verifier != nil {
		s.verifier.Start(ctx)
	}
}

// StartProjectSync launches the background project-mirror dispatcher. It runs
// until ctx is cancelled and is a no-op when sync is disabled
// (QUILL_TEMPO_SYNC_URL empty). Call once after New.
func (s *Server) StartProjectSync(ctx context.Context) {
	if s.projectSync == nil {
		return
	}
	go s.projectSync.Run(ctx)
}

// StartWorkItemRefs launches the background work-item-ref dispatcher. It runs
// until ctx is cancelled and is a no-op when the feature is disabled
// (QUILL_TEMPO_SYNC_REFS_URL empty). Call once after New.
func (s *Server) StartWorkItemRefs(ctx context.Context) {
	if s.workItemRefs == nil {
		return
	}
	go s.workItemRefs.Run(ctx)
}

// ServeHTTP implements http.Handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.handler.ServeHTTP(w, r)
}

func (s *Server) setupMiddleware() {
	s.router.Use(middleware.RequestID)
	s.router.Use(middleware.RealIP)
	s.router.Use(requestLogger(s.logger))
	s.router.Use(middleware.Recoverer)
	// Git transfers (clone/push of large repos) legitimately outlive the API
	// budget; the container's own request timeout still bounds them.
	s.router.Use(skipForGit(middleware.Timeout(60 * time.Second)))
	s.router.Use(securityHeaders)
	s.router.Use(cors.Handler(cors.Options{
		AllowedOrigins:   s.cfg.CORSAllowedOrigins,
		AllowedMethods:   []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-Request-Id"},
		ExposedHeaders:   []string{"X-Request-Id"},
		AllowCredentials: true,
		MaxAge:           300,
	}))
}

func (s *Server) setupRoutes() {
	// Operational endpoints (unauthenticated).
	s.router.Get("/healthz", s.handleHealth)
	s.router.Get("/readyz", s.handleReady)

	// Git smart-HTTP (serverless epic): mounted only when the own git engine
	// is on. B6 authenticates the surface: Basic credentials validate locally
	// (Quill git tokens) or via provider PAT introspection; anonymous callers
	// reach only public repos, and pushes require a write-level project role.
	if s.gitCache != nil {
		gitAuth := func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				ga, err := s.platform.AuthenticateGitRequest(r.Context(), r)
				if err != nil {
					w.Header().Set("WWW-Authenticate", `Basic realm="quill-git"`)
					http.Error(w, "authentication required", http.StatusUnauthorized)
					return
				}
				next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), gitActorKey{}, ga)))
			})
		}
		actorFrom := func(ctx context.Context) platform.GitActor {
			ga, _ := ctx.Value(gitActorKey{}).(platform.GitActor)
			return ga
		}
		// gitAccessErr converts the platform's denial semantics into the
		// engine's HTTP sentinels. An anonymous denial becomes a Basic
		// challenge so terminal/git clients prompt and retry — a 404 there
		// would strand a no-credentials clone of a private repo.
		gitAccessErr := func(ga platform.GitActor, err error) error {
			switch {
			case err == nil:
				return nil
			case errors.Is(err, platform.ErrGitUnauthorized):
				return gitengine.ErrGitUnauthorized
			case !ga.Authenticated:
				return gitengine.ErrGitUnauthorized
			case errors.Is(err, platform.ErrForbidden):
				return gitengine.ErrGitForbidden
			case errors.Is(err, platform.ErrNotFound):
				return gitengine.ErrGitNotFound
			default:
				return err
			}
		}
		// StripPrefix: chi's Mount keeps "/git" in r.URL.Path, and the engine
		// parses /{owner}/{repo}/... — without it every clone 404'd.
		s.router.Mount("/git", http.StripPrefix("/git", gitAuth(http.Handler(&gitengine.SmartHTTP{
			Hydrate: func(ctx context.Context, owner, repo string) (string, error) {
				ga := actorFrom(ctx)
				ref, err := s.platform.ResolveRepoForGitAccess(ctx, ga, owner, repo, false)
				if err != nil {
					return "", gitAccessErr(ga, err)
				}
				return s.gitCache.Hydrate(ctx, ref.TenantID.String(), ref.ProjectSlug, ref.RepoSlug)
			},
			Receive: &gitengine.ReceiveConfig{
				Engine: s.gitEngine,
				Resolve: func(ctx context.Context, owner, repo string) (gitengine.ReceiveContext, error) {
					ga := actorFrom(ctx)
					ref, err := s.platform.ResolveRepoForGitAccess(ctx, ga, owner, repo, true)
					if err != nil {
						return gitengine.ReceiveContext{}, gitAccessErr(ga, err)
					}
					dir, err := s.gitCache.Hydrate(ctx, ref.TenantID.String(), ref.ProjectSlug, ref.RepoSlug)
					if err != nil {
						return gitengine.ReceiveContext{}, err
					}
					return gitengine.ReceiveContext{
						ID: ref.RepoID.String(), Dir: dir,
						TenantID:    ref.TenantID.String(),
						ProjectSlug: ref.ProjectSlug, RepoSlug: ref.RepoSlug,
						ActorID: ga.UserID.String(),
					}, nil
				},
				Actor: func(r *http.Request) string {
					if ga := actorFrom(r.Context()); ga.Authenticated {
						return ga.Username
					}
					return "anonymous"
				},
				Referee:       s.platform.GitReferee(),
				Guard:         s.platform.GitPushGuard(),
				Leases:        s.platform.GitPushLeases(),
				OnPushSuccess: s.gitPushAuditor,
				OnRefsUpdated: s.gitPostReceive,
			},
		}))))
	}

	// Versioned API surface. Resource routes are added in later PRs.
	s.router.Route("/api/v1", func(r chi.Router) {
		r.Get("/meta", s.handleMeta)

		// Public profile pictures (rendered in <img> tags, no bearer).
		r.Get("/users/{id}/avatar", s.handleGetUserAvatar)

		// Forgejo webhook receiver: auto-triggers pipelines on push / pull_request.
		// Authenticated by an HMAC signature (QUILL_WEBHOOK_SECRET), not a JWT.
		r.Post("/webhooks/forgejo", s.handleWebhook)

		// Internal pipeline-queue surface for CI workers (serverless epic,
		// PR A1). Authenticated by X-Quill-Dispatch-Signature
		// (QUILL_PIPELINE_DISPATCH_SECRET) — no JWT, no tenant context: the
		// caller is trusted infrastructure, not a user.
		r.Route("/internal/dispatch", func(r chi.Router) {
			r.Post("/claim", s.handleDispatchClaim)
			r.Post("/logs", s.handleDispatchLogs)
			r.Post("/heartbeat", s.handleDispatchHeartbeat)
			r.Post("/report", s.handleDispatchReport)
		})

		// Creem merchant-of-record events (plan upgrades). Signature-verified
		// inside the handler; no Quill auth involved.
		r.Post("/webhooks/creem", s.handleCreemWebhook)

		// Authentication: /me requires a valid token. Register/login are handled by
		// Zitadel on the frontend; local auth routes are available as a fallback when
		// ZITADEL_ISSUER is not set (development / self-hosted without Zitadel).
		r.Route("/auth", func(r chi.Router) {
			if !s.externalAuthEnabled() {
				authLimiter := newIPRateLimiter(10, time.Minute)
				r.With(authLimiter.middleware()).Post("/register", s.handleRegister)
				r.With(authLimiter.middleware()).Post("/login", s.handleLogin)
			}
			// GitHub OAuth for onboarding repo import (no Quill auth required —
			// the user is redirected from the frontend during onboarding).
			r.Get("/github", s.handleGitHubOAuthRedirect)
			r.Get("/github/callback", s.handleGitHubOAuthCallback)
			r.Group(func(r chi.Router) {
				r.Use(s.requireAuth)
				r.Get("/me", s.handleMe)
				r.Patch("/me", s.handleUpdateProfile)
				r.Patch("/me/username", s.handleUpdateUsername)
				r.Patch("/me/email", s.handleUpdateEmail)
				r.Delete("/me", s.handleDeleteAccount)
				r.Post("/logout", s.handleLogout)
				if !s.externalAuthEnabled() {
					r.Patch("/me/password", s.handleChangePassword)
				}
			})
		})

		// Admin-only operations: requireAdmin enforces the admin check for the whole group.
		r.Group(func(r chi.Router) {
			r.Use(s.requireAuth)
			r.Use(s.requireAdmin)
			r.Get("/admin/users", s.handleListUsers)
			r.Patch("/admin/users/{username}/active", s.handleSetUserActive)
			if !s.externalAuthEnabled() {
				r.Post("/admin/users/{username}/reset-password", s.handleAdminResetPassword)
			}
			r.Get("/admin/audit-log", s.handleListAuditLog)
			r.Get("/admin/audit-log/export", s.handleExportAuditLog)
		})

		// Projects and repositories require authentication — except the
		// anonymous code-browsing reads of PUBLIC repositories (GH-style:
		// anyone can read an open-source repo without signing in). The
		// requireAuthOrPublicRead middleware falls through to requireAuth for
		// everything that isn't a repo read path, so the rest of this group is
		// unchanged; the platform layer enforces that only public repos are
		// actually served to anonymous callers.
		r.Group(func(r chi.Router) {
			r.Use(s.requireAuthOrPublicRead)
			// GitHub import (token comes from the OAuth cookie, not the Quill JWT).
			r.Get("/import/github/repos", s.handleListGitHubRepos)
			r.Post("/import/github", s.handleImportGitHubRepos)

			// Onboarding: provision the caller's personal project on demand.
			r.Post("/me/personal-project", s.handleCreatePersonalProject)

			r.Get("/me/export", s.handleExportMyData)
			r.Get("/me/pulls", s.handleListMyPulls)
			r.Get("/sbom/search", s.handleSearchSBOMs)
			r.Get("/me/pulls/open-count", s.handleOpenPullCount)
			r.Post("/me/git-token", s.handleCreateGitToken)
			r.Get("/me/git-tokens", s.handleListGitTokens)
			r.Delete("/me/git-tokens/{id}", s.handleRevokeGitToken)
			r.Get("/me/ssh-keys", s.handleListSSHKeys)
			r.Post("/me/ssh-keys", s.handleAddSSHKey)
			r.Delete("/me/ssh-keys/{id}", s.handleDeleteSSHKey)
			r.Get("/me/projects", s.handleListMyProjects)
			// Personal billing: the caller's own account plan, usage, and the
			// Pro upgrade checkout (per-account tenants from signup).
			r.Get("/me/billing", s.handleMyBilling)
			r.Post("/me/billing/checkout", s.handleMyBillingCheckout)
			r.Get("/me/contributions", s.handleMyContributions)
			r.Put("/me/avatar", s.handleUpdateAvatar)
			r.Delete("/me/avatar", s.handleDeleteAvatar)
			r.Route("/tenants", func(r chi.Router) {
				r.Route("/{tenant}", func(r chi.Router) {
					r.Route("/policies", func(r chi.Router) {
						r.Get("/", s.handleListTenantPolicies)
						r.Put("/", s.handleSetTenantPolicy)
						r.Delete("/", s.handleDeleteTenantPolicy)
					})
					r.Route("/environment-policies", func(r chi.Router) {
						r.Get("/", s.handleListTenantEnvironmentPolicies)
						r.Put("/", s.handleSetTenantEnvironmentPolicy)
						r.Delete("/", s.handleDeleteTenantEnvironmentPolicy)
					})
				})
			})
			r.Get("/orgs", s.handleListOrganizations)
			r.Post("/orgs", s.handleCreateOrganization)
			r.Route("/orgs/{slug}", func(r chi.Router) {
				r.Get("/members", s.handleListOrgMembers)
				r.Patch("/members/{userId}", s.handleUpdateOrgMemberRole)
				r.Delete("/members/{userId}", s.handleRemoveOrgMember)
				r.Get("/invites", s.handleListInvites)
				r.Post("/invites", s.handleCreateInvite)
				r.Delete("/invites/{id}", s.handleRevokeInvite)
				r.Get("/sso", s.handleGetOrgSSO)
				r.Put("/sso", s.handleSetOrgSSO)
				r.Delete("/sso", s.handleDeleteOrgSSO)
				r.Route("/groups", func(r chi.Router) {
					r.Get("/", handleGroupsList(s))
					r.Post("/", handleGroupsCreate(s))
					r.Route("/{group}", func(r chi.Router) {
						r.Patch("/", handleGroupsUpdate(s))
						r.Delete("/", handleGroupsDelete(s))
						r.Get("/members", handleGroupMembersList(s))
						r.Post("/members", handleGroupMembersAdd(s))
						r.Delete("/members/{userId}", handleGroupMembersRemove(s))
						r.Put("/projects/{project}", handleGroupProjectRoleSet(s))
						r.Delete("/projects/{project}", handleGroupProjectRoleRemove(s))
					})
				})
				r.Route("/billing", func(r chi.Router) {
					r.Get("/", handleBillingGet(s))
					r.Put("/plan", handleBillingSetPlan(s))
					r.Post("/checkout", handleBillingCheckout(s))
				})
			})
			r.Post("/invites/{token}/accept", s.handleAcceptInvite)
			r.Route("/projects", func(r chi.Router) {
				r.Get("/", s.handleListProjects)
				r.Post("/", s.handleCreateProject)
				r.Route("/{slug}", func(r chi.Router) {
					r.Get("/", s.handleGetProject)
					r.Route("/policies", func(r chi.Router) {
						r.Get("/", s.handleListProjectPolicies)
						r.Put("/", s.handleSetProjectPolicy)
						r.Delete("/", s.handleDeleteProjectPolicy)
					})
					r.Route("/environment-policies", func(r chi.Router) {
						r.Get("/", s.handleListProjectEnvironmentPolicies)
						r.Put("/", s.handleSetProjectEnvironmentPolicy)
						r.Delete("/", s.handleDeleteProjectEnvironmentPolicy)
					})
					r.Route("/secrets", func(r chi.Router) {
						r.Get("/", s.handleListProjectSecrets)
						r.Put("/{name}", s.handleSetProjectSecret)
						r.Delete("/{name}", s.handleDeleteProjectSecret)
					})
					r.Route("/environments", func(r chi.Router) {
						r.Get("/", s.handleListEnvironments)
						r.Post("/", s.handleCreateEnvironment)
						r.Route("/{env}", func(r chi.Router) {
							r.Get("/", s.handleGetEnvironment)
							r.Patch("/", s.handleUpdateEnvironment)
							r.Delete("/", s.handleDeleteEnvironment)
							r.Route("/secrets", func(r chi.Router) {
								r.Get("/", s.handleListEnvironmentSecrets)
								r.Put("/{name}", s.handleSetEnvironmentSecret)
								r.Delete("/{name}", s.handleDeleteEnvironmentSecret)
							})
						})
					})
					r.Route("/repos", func(r chi.Router) {
						r.Get("/", s.handleListRepos)
						r.Post("/", s.handleCreateRepo)
						r.Route("/{repo}", func(r chi.Router) {
							r.Get("/", s.handleGetRepo)
							r.Patch("/", s.handleUpdateRepo)
							r.Delete("/", s.handleDeleteRepo)
							r.Post("/fork", s.handleForkRepo)
							r.Put("/star", s.handleStarRepo)
							r.Delete("/star", s.handleUnstarRepo)
							r.Get("/branches", s.handleListBranches)
							r.Get("/commits", s.handleListCommits)
							r.Get("/commits/{sha}", s.handleGetCommit)
							r.Get("/contents", s.handleGetContents)
							r.Post("/markup", s.handleRenderMarkup)
							r.Get("/sbom", s.handleGetSBOM)
							r.Post("/sbom", s.handleRequestSBOM)
							r.Get("/sbom/download", s.handleDownloadSBOM)
							r.Get("/sbom/history", s.handleSBOMHistory)
							r.Route("/policies", func(r chi.Router) {
								r.Get("/", s.handleListBranchPolicies)
								r.Put("/", s.handleSetBranchPolicy)
								r.Delete("/", s.handleDeleteBranchPolicy)
							})
							r.Route("/environment-policies", func(r chi.Router) {
								r.Get("/", s.handleListEnvironmentPolicies)
								r.Put("/", s.handleSetEnvironmentPolicy)
								r.Delete("/", s.handleDeleteEnvironmentPolicy)
							})
							r.Route("/issues", func(r chi.Router) {
								r.Get("/", s.handleListIssues)
								r.Post("/", s.handleCreateIssue)
								r.Route("/{number}", func(r chi.Router) {
									r.Get("/", s.handleGetIssue)
									r.Patch("/", s.handleEditIssue)
									r.Post("/comments", s.handleCreateIssueComment)
								})
							})
							r.Route("/secrets", func(r chi.Router) {
								r.Get("/", s.handleListRepoSecrets)
								r.Put("/{name}", s.handleSetRepoSecret)
								r.Delete("/{name}", s.handleDeleteRepoSecret)
							})
							r.Route("/pipelines", func(r chi.Router) {
								r.Get("/", s.handleListPipelines)
								r.Post("/", s.handleTriggerRun)
								r.Get("/runs", s.handleListRuns)
								r.Get("/runs/{number}", s.handleGetRun)
								r.Get("/runs/{number}/logs", s.handleStreamRunLogs)
								r.Get("/runs/{number}/live", s.handleRunLive)
								r.Post("/runs/{number}/cancel", s.handleCancelRun)
							})
							r.Route("/pulls", func(r chi.Router) {
								r.Get("/", s.handleListPulls)
								r.Post("/", s.handleCreatePull)
								r.Route("/{number}", func(r chi.Router) {
									r.Get("/", s.handleGetPull)
									r.Get("/diff", s.handleGetPullDiff)
									r.Get("/commits", s.handleListPullCommits)
									r.Get("/comments", s.handleListPullComments)
									r.Post("/comments", s.handleCreatePullComment)
									r.Get("/line-comments", s.handleListLineComments)
									r.Post("/line-comments", s.handleCreateLineComment)
									r.Get("/reviews", s.handleListPullReviews)
									r.Post("/reviews", s.handleCreatePullReview)
									r.Post("/merge", s.handleMergePull)
								})
							})
						})
					})
				})
			})
		})
	})
}

// gitPushAuditor backs the engine's OnPushSuccess seam: tenant-scoped audit
// entry + daily push meter, off the push request's critical path budget
// (best-effort inside the handler, after truth committed).
func (s *Server) gitPushAuditor(rcx gitengine.ReceiveContext, actor string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tenantID := uuid.Nil
	if id, err := uuid.Parse(rcx.TenantID); err == nil {
		tenantID = id
	}
	var actorID uuid.NullUUID
	if rcx.ActorID != "" {
		if id, err := uuid.Parse(rcx.ActorID); err == nil {
			actorID = uuid.NullUUID{UUID: id, Valid: true}
		}
	}
	s.platform.LogGitPush(ctx, tenantID, actorID, actor, rcx.ProjectSlug, rcx.RepoSlug, "")
}

// skipForGit applies mw to every route except the /git smart-HTTP mount and
// the long-lived pipeline live feed (WebSocket).
func skipForGit(mw func(http.Handler) http.Handler) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		wrapped := mw(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/git" || strings.HasPrefix(r.URL.Path, "/git/") || isLiveFeed(r) {
				next.ServeHTTP(w, r)
				return
			}
			wrapped.ServeHTTP(w, r)
		})
	}
}
