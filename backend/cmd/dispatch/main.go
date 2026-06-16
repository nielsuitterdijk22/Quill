// Command dispatch is the standalone Quill pipeline dispatcher.
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

	"github.com/nielsuitterdijk22/quill/internal/dispatch"
	"github.com/nielsuitterdijk22/quill/internal/logging"
	"github.com/nielsuitterdijk22/quill/internal/pipeline"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "fatal:", err)
		os.Exit(1)
	}
}

func run() error {
	env := getenv("QUILL_ENV", "development")
	addr := getenv("QUILL_HTTP_ADDR", ":8090")
	readTimeout := getdur("QUILL_HTTP_READ_TIMEOUT", 15*time.Second)
	writeTimeout := getdur("QUILL_HTTP_WRITE_TIMEOUT", 30*time.Second)
	secret := getenv("QUILL_PIPELINE_DISPATCH_SECRET", "")

	logger := logging.New(getenv("QUILL_LOG_LEVEL", "info"), getenv("QUILL_LOG_FORMAT", "json"))
	logger.Info("starting quill pipeline dispatcher", "env", env, "addr", addr)

	httpServer := &http.Server{
		Addr:              addr,
		Handler:           dispatch.New(logger, pipeline.NewActRunner(), secret),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       120 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

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

func getenv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}

func getdur(key string, fallback time.Duration) time.Duration {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return fallback
}
