// Command projectsync-backfill is a one-shot admin tool that replays every
// existing Quill project through the project-mirror outbox as a create event,
// so projects that pre-date the mirror get synced to Tempo.
//
// It writes only outbox rows — the running api dispatcher (or a later run of the
// api server) delivers them. Safe to run repeatedly: a project that already has
// an outbox event is skipped, so a second run is a no-op.
//
// Usage:
//
//	QUILL_DATABASE_URL=... go run ./cmd/projectsync-backfill
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/nielsuitterdijk22/quill/internal/config"
	"github.com/nielsuitterdijk22/quill/internal/logging"
	"github.com/nielsuitterdijk22/quill/internal/projectsync"
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
	logger.Info("project sync backfill starting", "env", cfg.Env)

	// Apply migrations so the outbox table exists even on a fresh checkout.
	if err := store.Migrate(cfg.DatabaseURL); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}

	ctx := context.Background()
	st, err := store.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("connect store: %w", err)
	}
	defer st.Close()

	res, err := projectsync.Backfill(ctx, st, logger)
	if err != nil {
		return fmt.Errorf("backfill: %w", err)
	}
	logger.Info("project sync backfill complete",
		"total", res.Total, "enqueued", res.Enqueued, "skipped", res.Skipped)
	if cfg.TempoSync.URL == "" {
		logger.Warn("QUILL_TEMPO_SYNC_URL is empty; enqueued events will not be delivered until sync is configured")
	}
	return nil
}
