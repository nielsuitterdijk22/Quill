package main

import (
	"context"

	"github.com/google/uuid"

	"github.com/nielsuitterdijk22/quill/internal/store"
	"github.com/nielsuitterdijk22/quill/internal/store/db"
)

// storeAdapter adapts *store.Store to the storeLike surface createRepo needs.
// The store embeds *db.Queries (which satisfies dbQuerier) and provides the
// hand-written InTx / ListProjectsByTenant / CreateProject methods directly.
type storeAdapter struct {
	*store.Store
}

var _ storeLike = (*storeAdapter)(nil)

func newStoreAdapter(st *store.Store) storeLike {
	return &storeAdapter{Store: st}
}

// InTx runs fn inside a transaction, committing on success and rolling back on
// error. The store's own InTx hands fn a *db.Queries; the adapter adapts it to
// the dbQuerier shape createRepo's callback expects.
func (a *storeAdapter) InTx(ctx context.Context, fn func(q dbQuerier) error) error {
	return a.Store.InTx(ctx, func(q *db.Queries) error {
		return fn(q)
	})
}

// ListProjectsByTenant is a passthrough to the store's hand-written query.
func (a *storeAdapter) ListProjectsByTenant(ctx context.Context, tenantID uuid.UUID) ([]db.Project, error) {
	return a.Store.ListProjectsByTenant(ctx, tenantID)
}

// CreateProject is a passthrough to the generated query.
func (a *storeAdapter) CreateProject(ctx context.Context, arg db.CreateProjectParams) (db.Project, error) {
	return a.Store.CreateProject(ctx, arg)
}
