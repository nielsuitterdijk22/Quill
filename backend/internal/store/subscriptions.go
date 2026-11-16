package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// TenantSubscription is a tenant's trial window and Creem linkage
// (migration 000038). The zero value means "no row": no trial, no customer.
type TenantSubscription struct {
	TrialEndsAt         *time.Time
	CreemCustomerID     string
	CreemSubscriptionID string
}

// GetTenantSubscription returns the tenant's subscription row, or the zero
// value when none exists.
func (s *Store) GetTenantSubscription(ctx context.Context, tenantID uuid.UUID) (TenantSubscription, error) {
	var sub TenantSubscription
	err := s.pool.QueryRow(ctx, `
		SELECT trial_ends_at, creem_customer_id, creem_subscription_id
		FROM tenant_subscriptions WHERE tenant_id = $1`, tenantID).
		Scan(&sub.TrialEndsAt, &sub.CreemCustomerID, &sub.CreemSubscriptionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return TenantSubscription{}, nil
	}
	return sub, err
}

// StartTenantTrial puts the tenant on the 'trial' plan until endsAt.
func (s *Store) StartTenantTrial(ctx context.Context, tenantID uuid.UUID, endsAt time.Time) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `UPDATE tenants SET plan = 'trial', updated_at = now() WHERE id = $1`, tenantID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO tenant_subscriptions (tenant_id, trial_ends_at) VALUES ($1, $2)
		ON CONFLICT (tenant_id) DO UPDATE SET trial_ends_at = EXCLUDED.trial_ends_at, updated_at = now()`,
		tenantID, endsAt); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// SetTenantCreemIDs records the Creem customer/subscription behind a tenant's
// plan. Empty values leave the stored ones untouched.
func (s *Store) SetTenantCreemIDs(ctx context.Context, tenantID uuid.UUID, customerID, subscriptionID string) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO tenant_subscriptions (tenant_id, creem_customer_id, creem_subscription_id) VALUES ($1, $2, $3)
		ON CONFLICT (tenant_id) DO UPDATE SET
		    creem_customer_id = COALESCE(NULLIF(EXCLUDED.creem_customer_id, ''), tenant_subscriptions.creem_customer_id),
		    creem_subscription_id = COALESCE(NULLIF(EXCLUDED.creem_subscription_id, ''), tenant_subscriptions.creem_subscription_id),
		    updated_at = now()`, tenantID, customerID, subscriptionID)
	return err
}

// TenantIDByCreemSubscription finds the tenant whose plan a Creem subscription
// pays for (ok=false: unknown subscription).
func (s *Store) TenantIDByCreemSubscription(ctx context.Context, subscriptionID string) (uuid.UUID, bool, error) {
	if subscriptionID == "" {
		return uuid.Nil, false, nil
	}
	var id uuid.UUID
	err := s.pool.QueryRow(ctx,
		`SELECT tenant_id FROM tenant_subscriptions WHERE creem_subscription_id = $1`, subscriptionID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, false, nil
	}
	return id, err == nil, err
}
