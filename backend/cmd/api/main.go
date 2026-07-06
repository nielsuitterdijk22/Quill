// Command api is the Quill backend HTTP server.
//
// Quill is a version-control platform layered on top of Forgejo: Forgejo runs as
// a separate service and owns git storage and low-level repo/PR operations, while
// this service wraps Forgejo's REST API and stores Quill-specific metadata
// (orgs, teams, branch policies, pipelines, auth mapping) in Postgres.
package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/nielsuitterdijk22/quill/internal/config"
	"github.com/nielsuitterdijk22/quill/internal/logging"
	"github.com/nielsuitterdijk22/quill/internal/server"
	"github.com/nielsuitterdijk22/quill/internal/store"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "fatal:", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	logger := logging.New(cfg.LogLevel, cfg.LogFormat)
	logger.Info("starting quill api",
		"version", server.Version,
		"env", cfg.Env,
		"addr", cfg.HTTPAddr,
	)

	// Apply schema migrations before opening the pool for serving traffic.
	logger.Info("applying database migrations")
	if err := store.Migrate(cfg.DatabaseURL); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}

	setupCtx, cancelSetup := context.WithTimeout(context.Background(), 15*time.Second)
	st, err := store.New(setupCtx, cfg.DatabaseURL)
	cancelSetup()
	if err != nil {
		return fmt.Errorf("connect store: %w", err)
	}
	defer st.Close()
	logger.Info("database ready")

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	srv := server.New(cfg, logger, st)
	srv.StartAuth(ctx)
	srv.StartProjectSync(ctx)
	srv.StartWorkItemRefs(ctx)

	httpServer := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           srv,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       cfg.ReadTimeout,
		WriteTimeout:      cfg.WriteTimeout,
		IdleTimeout:       120 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return fmt.Errorf("http server: %w", err)
	case <-ctx.Done():
		logger.Info("shutdown signal received")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("graceful shutdown: %w", err)
	}
	logger.Info("shutdown complete")
	return nil
}
