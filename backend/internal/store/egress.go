package store

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// TenantEgress is an organization's stored egress policy (migration 000036).
type TenantEgress struct {
	AllowAll bool
	// Hosts nil = the built-in defaults.
	Hosts []string
	// Stored is false when the tenant never saved a policy.
	Stored bool
}

// GetTenantEgress returns a tenant's policy (zero value when unset).
func (s *Store) GetTenantEgress(ctx context.Context, tenantID uuid.UUID) (TenantEgress, error) {
	var e TenantEgress
	err := s.pool.QueryRow(ctx,
		`SELECT allow_all, hosts FROM tenant_egress_policies WHERE tenant_id = $1`, tenantID).Scan(&e.AllowAll, &e.Hosts)
	if errors.Is(err, pgx.ErrNoRows) {
		return TenantEgress{}, nil
	}
	e.Stored = err == nil
	return e, err
}

// SetTenantEgress upserts a tenant's policy; hosts nil resets to defaults.
func (s *Store) SetTenantEgress(ctx context.Context, tenantID, by uuid.UUID, allowAll bool, hosts []string) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO tenant_egress_policies (tenant_id, allow_all, hosts, updated_by)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (tenant_id) DO UPDATE
		SET allow_all = EXCLUDED.allow_all, hosts = EXCLUDED.hosts,
		    updated_by = EXCLUDED.updated_by, updated_at = now()`, tenantID, allowAll, hosts, by)
	return err
}

// GetRepoEgressHosts returns a repository's extra hosts.
func (s *Store) GetRepoEgressHosts(ctx context.Context, repoID uuid.UUID) ([]string, error) {
	var hosts []string
	err := s.pool.QueryRow(ctx, `SELECT hosts FROM repo_egress_hosts WHERE repo_id = $1`, repoID).Scan(&hosts)
	if errors.Is(err, pgx.ErrNoRows) {
		return []string{}, nil
	}
	return hosts, err
}

// SetRepoEgressHosts replaces a repository's extra hosts.
func (s *Store) SetRepoEgressHosts(ctx context.Context, repoID, by uuid.UUID, hosts []string) error {
	if hosts == nil {
		hosts = []string{}
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO repo_egress_hosts (repo_id, hosts, updated_by) VALUES ($1, $2, $3)
		ON CONFLICT (repo_id) DO UPDATE
		SET hosts = EXCLUDED.hosts, updated_by = EXCLUDED.updated_by, updated_at = now()`, repoID, hosts, by)
	return err
}
