package store

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
)

// TestMigrateToleratesNewerSchema: an older binary starting against a
// database a newer build already migrated must start (production outage
// 2026-11-16: "no migration found for version 41" on every cold start).
func TestMigrateToleratesNewerSchema(t *testing.T) {
	dsn := os.Getenv("QUILL_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("QUILL_TEST_DATABASE_URL not set")
	}
	if err := Migrate(dsn); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	ctx := context.Background()
	c, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	var v int64
	if err := c.QueryRow(ctx, `SELECT version FROM schema_migrations`).Scan(&v); err != nil {
		t.Fatal(err)
	}
	// Restore the real version, then close (Cleanup runs after the test body;
	// a deferred Close would run first and the restore would silently fail).
	t.Cleanup(func() {
		if _, err := c.Exec(context.Background(), `UPDATE schema_migrations SET version = $1, dirty = false`, v); err != nil {
			t.Errorf("restore schema version: %v", err)
		}
		_ = c.Close(context.Background())
	})

	// Pretend a newer build migrated one step further.
	if _, err := c.Exec(ctx, `UPDATE schema_migrations SET version = $1`, v+1); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(dsn); err != nil {
		t.Fatalf("older binary against newer schema must start, got %v", err)
	}
	// A dirty newer schema still fails loudly.
	if _, err := c.Exec(ctx, `UPDATE schema_migrations SET dirty = true`); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(dsn); err == nil {
		t.Fatal("dirty schema must not be ignored")
	}
}
