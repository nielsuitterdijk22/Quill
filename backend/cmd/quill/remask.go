package main

import (
	"context"
	"fmt"

	"github.com/nielsuitterdijk22/quill/internal/config"
	"github.com/nielsuitterdijk22/quill/internal/forgejo"
	"github.com/nielsuitterdijk22/quill/internal/logging"
	"github.com/nielsuitterdijk22/quill/internal/platform"
	"github.com/nielsuitterdijk22/quill/internal/store"
)

// runRemaskRunLogs repairs queue-mode runs reported before report-time secret
// masking (2026-11-13): masks their stored logs with each run's own secrets,
// then scrubs the secrets from the finished queue rows. Idempotent.
func runRemaskRunLogs(_ []string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	ctx := context.Background()
	st, err := store.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer st.Close()
	svc := platform.NewService(st, forgejo.New(config.ForgejoConfig{}), logging.New("info", "text"))
	runs, steps, err := svc.RemaskStoredRunLogs(ctx)
	if err != nil {
		return err
	}
	fmt.Printf("remask-run-logs: masked %d step log(s) across %d run(s); finished queue rows scrubbed of secrets\n", steps, runs)
	return nil
}
