package store

import (
	"database/sql"
	"errors"
	"fmt"
	"log/slog"

	"github.com/golang-migrate/migrate/v4"
	migratepgx "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" database/sql driver

	"github.com/nielsuitterdijk22/quill/internal/store/migrations"
)

// Migrate applies all pending "up" migrations embedded in the binary. It is
// idempotent: ErrNoChange (already up to date) is treated as success.
func Migrate(dsn string) error {
	src, err := iofs.New(migrations.FS, ".")
	if err != nil {
		return fmt.Errorf("open migration source: %w", err)
	}
	defer func() { _ = src.Close() }()

	sqlDB, err := sql.Open("pgx", dsn)
	if err != nil {
		return fmt.Errorf("open migrate connection: %w", err)
	}
	defer func() { _ = sqlDB.Close() }()

	driver, err := migratepgx.WithInstance(sqlDB, &migratepgx.Config{})
	if err != nil {
		return fmt.Errorf("create migrate driver: %w", err)
	}

	m, err := migrate.NewWithInstance("iofs", src, "pgx5", driver)
	if err != nil {
		return fmt.Errorf("init migrator: %w", err)
	}

	// A database that is AHEAD of this binary (a newer build migrated it, then
	// this older one starts — a rollback, or a cold start before the new
	// revision rolls out) must not stop the api: migrations here are additive,
	// so older code runs fine on a newer schema. golang-migrate would fail with
	// "no migration found for version N"; that took production down for ~15
	// minutes on 2026-11-16. Dirty states still fail loudly.
	if dbVersion, dirty, verr := m.Version(); verr == nil && !dirty {
		if latest, ok := latestVersion(src); ok && dbVersion > latest {
			slog.Warn("database schema is ahead of this binary; skipping migrations", "db_version", dbVersion, "binary_latest", latest)
			return nil
		}
	}

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("apply migrations: %w", err)
	}
	return nil
}

// latestVersion returns the highest migration version in the source.
func latestVersion(src source.Driver) (uint, bool) {
	v, err := src.First()
	if err != nil {
		return 0, false
	}
	for {
		next, err := src.Next(v)
		if err != nil {
			return v, true
		}
		v = next
	}
}
