// Package store is the Postgres data-access layer. It wraps a pgx connection
// pool and the sqlc-generated typed queries (internal/store/db), and runs schema
// migrations on startup.
package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nielsuitterdijk22/quill/internal/store/db"
)

// Store holds the connection pool and embeds the generated *db.Queries so callers
// can use typed queries directly (store.GetUserByID, store.CreateRepository, ...).
type Store struct {
	pool *pgxpool.Pool
	*db.Queries
}

// New opens a pgx pool against dsn and verifies connectivity.
func New(ctx context.Context, dsn string) (*Store, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("create pgx pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return &Store{pool: pool, Queries: db.New(pool)}, nil
}

// Ping checks database connectivity (used by the readiness probe).
func (s *Store) Ping(ctx context.Context) error {
	return s.pool.Ping(ctx)
}

// Close releases the pool.
func (s *Store) Close() {
	s.pool.Close()
}

// Pool exposes the underlying pool for advanced callers.
func (s *Store) Pool() *pgxpool.Pool { return s.pool }

// InTx runs fn inside a transaction, committing on success and rolling back on
// error. Use it for operations that must write several tables atomically (e.g.
// creating an org together with its first owning team and membership).
func (s *Store) InTx(ctx context.Context, fn func(q *db.Queries) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := fn(s.Queries.WithTx(tx)); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	return nil
}
