// Package server wires the HTTP router, middleware, and route handlers.
package server

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"

	"github.com/nielsuitterdijk22/quill/internal/auth"
	"github.com/nielsuitterdijk22/quill/internal/config"
	"github.com/nielsuitterdijk22/quill/internal/forgejo"
	"github.com/nielsuitterdijk22/quill/internal/pipeline"
	"github.com/nielsuitterdijk22/quill/internal/platform"
	"github.com/nielsuitterdijk22/quill/internal/store"
)

// Version is the API version reported by the /api/v1/meta endpoint.
const Version = "0.1.0"

// Server is the root HTTP handler for the Quill backend.
type Server struct {
	cfg         *config.Config
	logger      *slog.Logger
	store       *store.Store
	auth        *auth.Service
	verifier    auth.TokenVerifier
	forgejo     *forgejo.Client
	platform    *platform.Service
	router      chi.Router
	markupCache *markupCache
}

// externalAuthEnabled reports whether an external IdP (Clerk or Zitadel) is
// configured and ready. When false, Quill falls back to local username/password
// auth (register/login/password routes are registered).
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

	// Select the external IdP by QUILL_AUTH_PROVIDER. Clerk is the default; both
	// providers verify a bearer JWT, provision the user on first login, and map
	// the org claim to a Quill tenant. Personal project creation happens during
	// onboarding, so neither path creates one here.
	var verifier auth.TokenVerifier
	switch cfg.AuthProvider {
	case auth.ProviderZitadel:
		if cfg.Zitadel.Issuer != "" {
			verifier = auth.NewZitadelVerifier(cfg.Zitadel, st, logger).WithForgejo(fj)
		}
	default: // clerk
		if cfg.Clerk.FrontendAPI != "" {
			verifier = auth.NewClerkVerifier(cfg.Clerk, st, logger).WithForgejo(fj)
		}
	}

	s := &Server{
		cfg:         cfg,
		logger:      logger,
		store:       st,
		auth:        auth.NewService(st, auth.NewLocalProvider(st), auth.NewTokenService(cfg.JWT)).WithForgejo(fj, logger),
		verifier:    verifier,
		forgejo:     fj,
		platform:    platformSvc,
		router:      chi.NewRouter(),
		markupCache: newMarkupCache(),
	}
	s.setupMiddleware()
	s.setupRoutes()
	return s
}

// StartAuth begins background JWKS refresh for the configured external IdP's
// JWT verification. It must be called once the server's context is available
// (i.e. after New). No-op when only local auth is configured.
func (s *Server) StartAuth(ctx context.Context) {
	if s.verifier != nil {
		s.verifier.Start(ctx)
	}
}

// ServeHTTP implements http.Handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.router.ServeHTTP(w, r)
}

func (s *Server) setupMiddleware() {
	s.router.Use(middleware.RequestID)
	s.router.Use(middleware.RealIP)
	s.router.Use(requestLogger(s.logger))
	s.router.Use(middleware.Recoverer)
	s.router.Use(middleware.Timeout(60 * time.Second))
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

	// Versioned API surface. Resource routes are added in later PRs.
	s.router.Route("/api/v1", func(r chi.Router) {
		r.Get("/meta", s.handleMeta)

		// Forgejo webhook receiver: auto-triggers pipelines on push / pull_request.
		// Authenticated by an HMAC signature (QUILL_WEBHOOK_SECRET), not a JWT.
		r.Post("/webhooks/forgejo", s.handleWebhook)

		// Authentication: /me requires a valid token. Register/login are handled by
		// Clerk on the frontend; local auth routes are available as a fallback when
		// QUILL_CLERK_FRONTEND_API is not set (development / self-hosted without Clerk).
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

		// Projects and repositories require authentication.
		r.Group(func(r chi.Router) {
			r.Use(s.requireAuth)
			// GitHub import (token comes from the OAuth cookie, not the Quill JWT).
			r.Get("/import/github/repos", s.handleListGitHubRepos)
			r.Post("/import/github", s.handleImportGitHubRepos)

			// Onboarding: provision the caller's personal project on demand.
			r.Post("/me/personal-project", s.handleCreatePersonalProject)

			r.Get("/me/export", s.handleExportMyData)
			r.Get("/me/pulls", s.handleListMyPulls)
			r.Get("/me/pulls/open-count", s.handleOpenPullCount)
			r.Post("/me/git-token", s.handleCreateGitToken)
			r.Get("/me/git-tokens", s.handleListGitTokens)
			r.Delete("/me/git-tokens/{id}", s.handleRevokeGitToken)
			r.Get("/me/ssh-keys", s.handleListSSHKeys)
			r.Post("/me/ssh-keys", s.handleAddSSHKey)
			r.Delete("/me/ssh-keys/{id}", s.handleDeleteSSHKey)
			r.Get("/me/projects", s.handleListMyProjects)
			r.Get("/me/contributions", s.handleMyContributions)
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
					r.Route("/environments", func(r chi.Router) {
						r.Get("/", s.handleListEnvironments)
						r.Post("/", s.handleCreateEnvironment)
						r.Route("/{env}", func(r chi.Router) {
							r.Get("/", s.handleGetEnvironment)
							r.Patch("/", s.handleUpdateEnvironment)
							r.Delete("/", s.handleDeleteEnvironment)
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
							r.Route("/pipelines", func(r chi.Router) {
								r.Get("/", s.handleListPipelines)
								r.Post("/", s.handleTriggerRun)
								r.Get("/runs", s.handleListRuns)
								r.Get("/runs/{number}", s.handleGetRun)
								r.Get("/runs/{number}/logs", s.handleStreamRunLogs)
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
