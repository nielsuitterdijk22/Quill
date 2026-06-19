// Package server wires the HTTP router, middleware, and route handlers.
package server

import (
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
	cfg      *config.Config
	logger   *slog.Logger
	store    *store.Store
	auth     *auth.Service
	forgejo  *forgejo.Client
	platform *platform.Service
	router   chi.Router
}

// New constructs a Server with middleware and routes configured. store may be
// nil in tests that only exercise handlers which don't touch the database.
func New(cfg *config.Config, logger *slog.Logger, st *store.Store) *Server {
	fj := forgejo.New(cfg.Forgejo)
	platformSvc := platform.NewService(st, fj, logger)
	if cfg.Pipeline.DispatchURL != "" {
		platformSvc.WithRunner(pipeline.NewHTTPRunner(cfg.Pipeline.DispatchURL, cfg.Pipeline.DispatchSecret))
	}
	s := &Server{
		cfg:      cfg,
		logger:   logger,
		store:    st,
		auth:     auth.NewService(st, auth.NewLocalProvider(st), auth.NewTokenService(cfg.JWT)).WithForgejo(fj, logger),
		forgejo:  fj,
		platform: platformSvc,
		router:   chi.NewRouter(),
	}
	s.setupMiddleware()
	s.setupRoutes()
	return s
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

		// Authentication: register and login are public; me and logout require a token.
		r.Route("/auth", func(r chi.Router) {
			r.Post("/register", s.handleRegister)
			r.Post("/login", s.handleLogin)
			r.Group(func(r chi.Router) {
				r.Use(s.requireAuth)
				r.Get("/me", s.handleMe)
				r.Patch("/me", s.handleUpdateProfile)
				r.Post("/logout", s.handleLogout)
			})
		})

		// Projects and repositories require authentication.
		r.Group(func(r chi.Router) {
			r.Use(s.requireAuth)
			r.Get("/me/pulls", s.handleListMyPulls)
			r.Get("/me/pulls/open-count", s.handleOpenPullCount)
			r.Post("/me/git-token", s.handleCreateGitToken)
			r.Get("/me/git-tokens", s.handleListGitTokens)
			r.Delete("/me/git-tokens/{id}", s.handleRevokeGitToken)
			r.Get("/me/projects", s.handleListMyProjects)
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
							r.Route("/pipelines", func(r chi.Router) {
								r.Get("/", s.handleListPipelines)
								r.Post("/", s.handleTriggerRun)
								r.Get("/runs", s.handleListRuns)
								r.Get("/runs/{number}", s.handleGetRun)
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
