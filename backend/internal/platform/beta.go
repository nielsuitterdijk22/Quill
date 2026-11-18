package platform

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nielsuitterdijk22/quill/internal/store"
)

// Beta invites let the owner hand-pick free users: a platform admin mints a
// temporary, capped link; whoever redeems it while signed in has their
// personal account moved to the 'beta' plan (Pro limits, no charge).
const (
	betaDefaultDays = 14
	betaMaxDays     = 90
	betaMaxUses     = 100
)

// BetaInviteView is an invite's metadata for admins (never the code).
type BetaInviteView struct {
	ID        uuid.UUID  `json:"id"`
	Note      string     `json:"note"`
	MaxUses   int        `json:"maxUses"`
	Uses      int        `json:"uses"`
	ExpiresAt time.Time  `json:"expiresAt"`
	RevokedAt *time.Time `json:"revokedAt,omitempty"`
	CreatedAt time.Time  `json:"createdAt"`
	Active    bool       `json:"active"`
}

// CreateBetaInvite mints an invite code (platform admin). The raw code is
// returned once; only its hash is stored. days<=0 uses the default window.
func (s *Service) CreateBetaInvite(ctx context.Context, actor Actor, note string, maxUses, days int) (BetaInviteView, string, error) {
	if err := s.authorizePlatformAdmin(actor); err != nil {
		return BetaInviteView{}, "", err
	}
	if maxUses < 1 {
		maxUses = 1
	}
	if days < 1 {
		days = betaDefaultDays
	}
	if maxUses > betaMaxUses || days > betaMaxDays {
		return BetaInviteView{}, "", fmt.Errorf("%w: at most %d uses and %d days per invite", ErrInvalidInput, betaMaxUses, betaMaxDays)
	}
	raw, err := randomToken(18)
	if err != nil {
		return BetaInviteView{}, "", fmt.Errorf("generate code: %w", err)
	}
	code := "qb_" + raw
	expires := s.now().Add(time.Duration(days) * 24 * time.Hour)
	inv, err := s.store.CreateBetaInvite(ctx, hashToken(code), strings.TrimSpace(note), maxUses, expires,
		uuid.NullUUID{UUID: actor.UserID, Valid: actor.UserID != uuid.Nil})
	if err != nil {
		return BetaInviteView{}, "", fmt.Errorf("create beta invite: %w", err)
	}
	return s.betaView(inv), code, nil
}

// ListBetaInvites lists invites (platform admin).
func (s *Service) ListBetaInvites(ctx context.Context, actor Actor) ([]BetaInviteView, error) {
	if err := s.authorizePlatformAdmin(actor); err != nil {
		return nil, err
	}
	rows, err := s.store.ListBetaInvites(ctx)
	if err != nil {
		return nil, fmt.Errorf("list beta invites: %w", err)
	}
	out := make([]BetaInviteView, 0, len(rows))
	for _, r := range rows {
		out = append(out, s.betaView(r))
	}
	return out, nil
}

// RevokeBetaInvite disables an invite (platform admin). Accounts that already
// redeemed it keep their beta plan.
func (s *Service) RevokeBetaInvite(ctx context.Context, actor Actor, id uuid.UUID) error {
	if err := s.authorizePlatformAdmin(actor); err != nil {
		return err
	}
	ok, err := s.store.RevokeBetaInvite(ctx, id)
	if err != nil {
		return fmt.Errorf("revoke beta invite: %w", err)
	}
	if !ok {
		return ErrNotFound
	}
	return nil
}

// RedeemBetaInvite moves the caller's personal account to the beta plan.
// Accounts on a paid or vendor plan are refused (a beta grant must never
// replace or shadow a subscription).
func (s *Service) RedeemBetaInvite(ctx context.Context, actor Actor, code string) error {
	user, err := s.store.GetUserByID(ctx, actor.UserID)
	if err != nil {
		return fmt.Errorf("lookup user: %w", err)
	}
	if !user.TenantID.Valid {
		return fmt.Errorf("%w: this account predates personal plans and is already unlimited", ErrInvalidInput)
	}
	err = s.store.RedeemBetaInvite(ctx, hashToken(strings.TrimSpace(code)), user.TenantID.UUID, user.ID, s.now())
	switch {
	case errors.Is(err, store.ErrBetaInviteInvalid):
		return fmt.Errorf("%w: this beta invite is invalid, expired or already used up", ErrInvalidInput)
	case errors.Is(err, store.ErrBetaNotEligible):
		return fmt.Errorf("%w: this account already has a plan", ErrConflict)
	case err != nil:
		return fmt.Errorf("redeem beta invite: %w", err)
	}
	return nil
}

func (s *Service) betaView(i store.BetaInvite) BetaInviteView {
	return BetaInviteView{
		ID: i.ID, Note: i.Note, MaxUses: i.MaxUses, Uses: i.Uses, ExpiresAt: i.ExpiresAt,
		RevokedAt: i.RevokedAt, CreatedAt: i.CreatedAt,
		Active: i.RevokedAt == nil && i.Uses < i.MaxUses && s.now().Before(i.ExpiresAt),
	}
}
