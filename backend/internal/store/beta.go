package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Beta redemption outcomes.
var (
	// ErrBetaInviteInvalid: unknown, revoked, expired or used-up code.
	ErrBetaInviteInvalid = errors.New("beta invite is invalid, expired or used up")
	// ErrBetaNotEligible: the account already has a plan that a beta grant
	// must not replace.
	ErrBetaNotEligible = errors.New("account is not eligible for a beta invite")
)

// BetaInvite is an invite's metadata (never the code itself).
type BetaInvite struct {
	ID        uuid.UUID
	Note      string
	MaxUses   int
	Uses      int
	ExpiresAt time.Time
	RevokedAt *time.Time
	CreatedAt time.Time
}

// CreateBetaInvite stores a hashed invite code.
func (s *Store) CreateBetaInvite(ctx context.Context, codeHash, note string, maxUses int, expiresAt time.Time, createdBy uuid.NullUUID) (BetaInvite, error) {
	inv := BetaInvite{Note: note, MaxUses: maxUses, ExpiresAt: expiresAt}
	err := s.pool.QueryRow(ctx, `
		INSERT INTO beta_invites (code_hash, note, max_uses, expires_at, created_by)
		VALUES ($1, $2, $3, $4, $5) RETURNING id, created_at`,
		codeHash, note, maxUses, expiresAt, createdBy).Scan(&inv.ID, &inv.CreatedAt)
	return inv, err
}

// ListBetaInvites returns every invite, newest first.
func (s *Store) ListBetaInvites(ctx context.Context) ([]BetaInvite, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, note, max_uses, uses, expires_at, revoked_at, created_at
		FROM beta_invites ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []BetaInvite
	for rows.Next() {
		var i BetaInvite
		if err := rows.Scan(&i.ID, &i.Note, &i.MaxUses, &i.Uses, &i.ExpiresAt, &i.RevokedAt, &i.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, rows.Err()
}

// RevokeBetaInvite disables an invite (idempotent; false: no such invite).
func (s *Store) RevokeBetaInvite(ctx context.Context, id uuid.UUID) (bool, error) {
	tag, err := s.pool.Exec(ctx, `UPDATE beta_invites SET revoked_at = COALESCE(revoked_at, now()) WHERE id = $1`, id)
	return tag.RowsAffected() > 0, err
}

// RedeemBetaInvite atomically consumes one use of the invite and moves the
// tenant from a locked plan ('inactive'/'lapsed') to 'beta'. Re-redeeming on
// an account that is already on beta is a no-op success that consumes nothing.
func (s *Store) RedeemBetaInvite(ctx context.Context, codeHash string, tenantID, userID uuid.UUID, now time.Time) error {
	return s.InTxTx(ctx, func(tx pgx.Tx) error {
		var plan string
		if err := tx.QueryRow(ctx, `SELECT plan FROM tenants WHERE id = $1 FOR UPDATE`, tenantID).Scan(&plan); err != nil {
			return err
		}
		switch plan {
		case "beta":
			return nil
		case "inactive", "lapsed":
		default:
			return ErrBetaNotEligible
		}
		var inviteID uuid.UUID
		err := tx.QueryRow(ctx, `
			UPDATE beta_invites SET uses = uses + 1
			WHERE code_hash = $1 AND revoked_at IS NULL AND expires_at > $2 AND uses < max_uses
			RETURNING id`, codeHash, now).Scan(&inviteID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrBetaInviteInvalid
		}
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE tenants SET plan = 'beta', updated_at = now() WHERE id = $1`, tenantID); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO beta_invite_redemptions (tenant_id, invite_id, redeemed_by) VALUES ($1, $2, $3)
			ON CONFLICT (tenant_id) DO UPDATE SET invite_id = EXCLUDED.invite_id, redeemed_by = EXCLUDED.redeemed_by, redeemed_at = now()`,
			tenantID, inviteID, userID)
		return err
	})
}
