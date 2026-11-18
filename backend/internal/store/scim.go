package store

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ErrSCIMNotFound is returned when a SCIM resource does not exist in the
// tenant (or exists only in another tenant).
var ErrSCIMNotFound = errors.New("scim resource not found")

// SCIMToken is a provisioning token's metadata (never the secret).
type SCIMToken struct {
	ID         uuid.UUID
	Name       string
	CreatedAt  time.Time
	LastUsedAt *time.Time
}

// CreateSCIMToken stores a hashed provisioning token for a tenant.
func (s *Store) CreateSCIMToken(ctx context.Context, tenantID uuid.UUID, name, tokenHash string, createdBy uuid.NullUUID) (SCIMToken, error) {
	t := SCIMToken{Name: name}
	err := s.pool.QueryRow(ctx, `
		INSERT INTO scim_tokens (tenant_id, name, token_hash, created_by) VALUES ($1, $2, $3, $4)
		RETURNING id, created_at`, tenantID, name, tokenHash, createdBy).Scan(&t.ID, &t.CreatedAt)
	return t, err
}

// ListSCIMTokens lists a tenant's provisioning tokens, newest first.
func (s *Store) ListSCIMTokens(ctx context.Context, tenantID uuid.UUID) ([]SCIMToken, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, name, created_at, last_used_at FROM scim_tokens
		WHERE tenant_id = $1 ORDER BY created_at DESC`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SCIMToken
	for rows.Next() {
		var t SCIMToken
		if err := rows.Scan(&t.ID, &t.Name, &t.CreatedAt, &t.LastUsedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// DeleteSCIMToken revokes one of a tenant's tokens (ok=false: no such token
// in this tenant).
func (s *Store) DeleteSCIMToken(ctx context.Context, tenantID, id uuid.UUID) (bool, error) {
	tag, err := s.pool.Exec(ctx, `DELETE FROM scim_tokens WHERE id = $1 AND tenant_id = $2`, id, tenantID)
	return tag.RowsAffected() > 0, err
}

// SCIMTenantByTokenHash resolves a bearer token to its tenant and stamps its
// last use (ok=false: unknown token).
func (s *Store) SCIMTenantByTokenHash(ctx context.Context, tokenHash string) (uuid.UUID, bool, error) {
	var tenantID uuid.UUID
	err := s.pool.QueryRow(ctx, `
		UPDATE scim_tokens SET last_used_at = now() WHERE token_hash = $1 RETURNING tenant_id`, tokenHash).Scan(&tenantID)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, false, nil
	}
	return tenantID, err == nil, err
}

// SCIMUser is an org's provisioning record for one person.
type SCIMUser struct {
	ID          uuid.UUID
	TenantID    uuid.UUID
	UserID      uuid.UUID
	UserName    string
	ExternalID  string
	Email       string
	GivenName   string
	FamilyName  string
	DisplayName string
	Active      bool
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

const scimUserCols = `id, tenant_id, user_id, user_name, external_id, email, given_name, family_name, display_name, active, created_at, updated_at`

func scanSCIMUser(row pgx.Row) (SCIMUser, error) {
	var u SCIMUser
	err := row.Scan(&u.ID, &u.TenantID, &u.UserID, &u.UserName, &u.ExternalID, &u.Email, &u.GivenName, &u.FamilyName, &u.DisplayName, &u.Active, &u.CreatedAt, &u.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return SCIMUser{}, ErrSCIMNotFound
	}
	return u, err
}

// InsertSCIMUser records a provisioned person. Unique violations (userName or
// person already provisioned in the tenant) surface to the caller.
func (s *Store) InsertSCIMUser(ctx context.Context, u SCIMUser) (SCIMUser, error) {
	return scanSCIMUser(s.pool.QueryRow(ctx, `
		INSERT INTO scim_users (tenant_id, user_id, user_name, external_id, email, given_name, family_name, display_name, active)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) RETURNING `+scimUserCols,
		u.TenantID, u.UserID, u.UserName, u.ExternalID, u.Email, u.GivenName, u.FamilyName, u.DisplayName, u.Active))
}

// UpdateSCIMUser replaces a provisioning record's mutable fields.
func (s *Store) UpdateSCIMUser(ctx context.Context, u SCIMUser) (SCIMUser, error) {
	return scanSCIMUser(s.pool.QueryRow(ctx, `
		UPDATE scim_users SET user_name = $3, external_id = $4, email = $5, given_name = $6, family_name = $7,
		    display_name = $8, active = $9, updated_at = now()
		WHERE id = $1 AND tenant_id = $2 RETURNING `+scimUserCols,
		u.ID, u.TenantID, u.UserName, u.ExternalID, u.Email, u.GivenName, u.FamilyName, u.DisplayName, u.Active))
}

// GetSCIMUser loads one record, scoped to the tenant.
func (s *Store) GetSCIMUser(ctx context.Context, tenantID, id uuid.UUID) (SCIMUser, error) {
	return scanSCIMUser(s.pool.QueryRow(ctx, `SELECT `+scimUserCols+` FROM scim_users WHERE id = $1 AND tenant_id = $2`, id, tenantID))
}

// DeleteSCIMUser removes a provisioning record.
func (s *Store) DeleteSCIMUser(ctx context.Context, tenantID, id uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM scim_users WHERE id = $1 AND tenant_id = $2`, id, tenantID)
	return err
}

// SCIMUserFilter narrows a listing; the zero value lists everything.
type SCIMUserFilter struct {
	Attr  string // "userName" | "externalId" | "email" | "id"
	Value string
}

// ListSCIMUsers pages a tenant's records (offset is 0-based) and returns the
// total match count.
func (s *Store) ListSCIMUsers(ctx context.Context, tenantID uuid.UUID, f SCIMUserFilter, offset, limit int) ([]SCIMUser, int, error) {
	where := `tenant_id = $1`
	args := []any{tenantID}
	switch f.Attr {
	case "userName":
		where += ` AND lower(user_name) = lower($2)`
		args = append(args, f.Value)
	case "externalId":
		where += ` AND external_id = $2`
		args = append(args, f.Value)
	case "email":
		where += ` AND lower(email) = lower($2)`
		args = append(args, f.Value)
	case "id":
		id, err := uuid.Parse(f.Value)
		if err != nil {
			return nil, 0, nil
		}
		where += ` AND id = $2`
		args = append(args, id)
	}
	var total int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM scim_users WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, limit, offset)
	n := len(args)
	rows, err := s.pool.Query(ctx, `SELECT `+scimUserCols+` FROM scim_users WHERE `+where+
		` ORDER BY created_at, id LIMIT $`+itoa(n-1)+` OFFSET $`+itoa(n), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []SCIMUser
	for rows.Next() {
		u, err := scanSCIMUser(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, u)
	}
	return out, total, rows.Err()
}

// SCIMGroup is an IdP-owned Quill group.
type SCIMGroup struct {
	ID          uuid.UUID
	TenantID    uuid.UUID
	GroupID     uuid.UUID
	DisplayName string
	ExternalID  string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

const scimGroupCols = `id, tenant_id, group_id, display_name, external_id, created_at, updated_at`

func scanSCIMGroup(row pgx.Row) (SCIMGroup, error) {
	var g SCIMGroup
	err := row.Scan(&g.ID, &g.TenantID, &g.GroupID, &g.DisplayName, &g.ExternalID, &g.CreatedAt, &g.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return SCIMGroup{}, ErrSCIMNotFound
	}
	return g, err
}

// InsertSCIMGroup records an IdP-owned group.
func (s *Store) InsertSCIMGroup(ctx context.Context, g SCIMGroup) (SCIMGroup, error) {
	return scanSCIMGroup(s.pool.QueryRow(ctx, `
		INSERT INTO scim_groups (tenant_id, group_id, display_name, external_id) VALUES ($1, $2, $3, $4)
		RETURNING `+scimGroupCols, g.TenantID, g.GroupID, g.DisplayName, g.ExternalID))
}

// UpdateSCIMGroup replaces a group's mutable fields (and renames the Quill group).
func (s *Store) UpdateSCIMGroup(ctx context.Context, g SCIMGroup) (SCIMGroup, error) {
	out, err := scanSCIMGroup(s.pool.QueryRow(ctx, `
		UPDATE scim_groups SET display_name = $3, external_id = $4, updated_at = now()
		WHERE id = $1 AND tenant_id = $2 RETURNING `+scimGroupCols, g.ID, g.TenantID, g.DisplayName, g.ExternalID))
	if err != nil {
		return out, err
	}
	_, err = s.pool.Exec(ctx, `UPDATE groups SET name = $2, updated_at = now() WHERE id = $1`, out.GroupID, out.DisplayName)
	return out, err
}

// GetSCIMGroup loads one group, scoped to the tenant.
func (s *Store) GetSCIMGroup(ctx context.Context, tenantID, id uuid.UUID) (SCIMGroup, error) {
	return scanSCIMGroup(s.pool.QueryRow(ctx, `SELECT `+scimGroupCols+` FROM scim_groups WHERE id = $1 AND tenant_id = $2`, id, tenantID))
}

// ListSCIMGroups pages a tenant's groups, optionally filtered by
// displayName/externalId/id equality.
func (s *Store) ListSCIMGroups(ctx context.Context, tenantID uuid.UUID, f SCIMUserFilter, offset, limit int) ([]SCIMGroup, int, error) {
	where := `tenant_id = $1`
	args := []any{tenantID}
	switch f.Attr {
	case "displayName":
		where += ` AND lower(display_name) = lower($2)`
		args = append(args, f.Value)
	case "externalId":
		where += ` AND external_id = $2`
		args = append(args, f.Value)
	case "id":
		id, err := uuid.Parse(f.Value)
		if err != nil {
			return nil, 0, nil
		}
		where += ` AND id = $2`
		args = append(args, id)
	}
	var total int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM scim_groups WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, limit, offset)
	n := len(args)
	rows, err := s.pool.Query(ctx, `SELECT `+scimGroupCols+` FROM scim_groups WHERE `+where+
		` ORDER BY created_at, id LIMIT $`+itoa(n-1)+` OFFSET $`+itoa(n), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []SCIMGroup
	for rows.Next() {
		g, err := scanSCIMGroup(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, g)
	}
	return out, total, rows.Err()
}

// SCIMGroupMembers returns the SCIM user ids (in the same tenant) that are
// members of a SCIM group.
func (s *Store) SCIMGroupMembers(ctx context.Context, tenantID, groupID uuid.UUID) ([]SCIMUser, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+prefixCols("su.", scimUserCols)+` FROM group_members gm
		JOIN scim_users su ON su.user_id = gm.user_id AND su.tenant_id = $1
		WHERE gm.group_id = $2 ORDER BY su.created_at`, tenantID, groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SCIMUser
	for rows.Next() {
		u, err := scanSCIMUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// AddTenantMemberIfAbsent makes a user a plain member of a tenant without
// touching an existing membership's role (reports whether a row was added).
func (s *Store) AddTenantMemberIfAbsent(ctx context.Context, tenantID, userID uuid.UUID) (bool, error) {
	tag, err := s.pool.Exec(ctx, `
		INSERT INTO tenant_members (tenant_id, user_id, role) VALUES ($1, $2, 'member')
		ON CONFLICT DO NOTHING`, tenantID, userID)
	return tag.RowsAffected() > 0, err
}

// RemoveUserFromTenant offboards a user from a tenant: it drops the tenant
// membership, their membership of every group in that tenant, and their direct
// project_members rows on that tenant's projects (the creator's owner row
// included), all in one transaction. Rows in other tenants — their personal
// project — are untouched. A later re-join starts from the plain member role,
// never from a role held before leaving. Reports whether a membership existed.
func (s *Store) RemoveUserFromTenant(ctx context.Context, tenantID, userID uuid.UUID) (bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `
		DELETE FROM group_members WHERE user_id = $2
		AND group_id IN (SELECT id FROM groups WHERE tenant_id = $1)`, tenantID, userID); err != nil {
		return false, err
	}
	if _, err := tx.Exec(ctx, `
		DELETE FROM project_members WHERE user_id = $2
		AND project_id IN (SELECT id FROM projects WHERE tenant_id = $1)`, tenantID, userID); err != nil {
		return false, err
	}
	tag, err := tx.Exec(ctx, `DELETE FROM tenant_members WHERE tenant_id = $1 AND user_id = $2`, tenantID, userID)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, tx.Commit(ctx)
}

func itoa(n int) string { return strconv.Itoa(n) }

// prefixCols qualifies each column of a comma-separated list with prefix.
func prefixCols(prefix, cols string) string {
	parts := strings.Split(cols, ",")
	for i, c := range parts {
		parts[i] = prefix + strings.TrimSpace(c)
	}
	return strings.Join(parts, ", ")
}
