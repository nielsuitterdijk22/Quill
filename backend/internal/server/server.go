// Package server wires the HTTP router, middleware, and route handlers.
package server

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"

	"github.com/nielsuitterdijk22/quill/internal/config"
)

// Version is the API version reported by the /api/v1/meta endpoint.
const Version = "0.1.0"

// Server is the root HTTP handler for the Quill backend.
type Server struct {
	cfg    *config.Config
	logger *slog.Logger
	router chi.Router
}

// New constructs a Server with middleware and routes configured.
func New(cfg *config.Config, logger *slog.Logger) *Server {
	s := &Server{
		cfg:    cfg,
		logger: logger,
		router: chi.NewRouter(),
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
	})
}
