package platform

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nielsuitterdijk22/quill/internal/store/db"
)

// betaAccount provisions a per-account tenant the way a first login does.
func betaAccount(t *testing.T, svc *Service, name string) (Actor, db.Tenant) {
	t.Helper()
	st := svc.store
	ctx := context.Background()
	userID := scopeMakeUser(t, st, name)
	tenant, err := st.CreateTenant(ctx, db.CreateTenantParams{Slug: name, Name: name})
	if err != nil {
		t.Fatalf("create account tenant: %v", err)
	}
	if _, err := st.SetUserTenant(ctx, db.SetUserTenantParams{ID: userID, TenantID: uuid.NullUUID{UUID: tenant.ID, Valid: true}}); err != nil {
		t.Fatalf("link tenant: %v", err)
	}
	return Actor{UserID: userID}, tenant
}

// TestBetaInviteLifecycle: only platform admins mint invites; a code moves an
// inactive account to the beta plan (Pro limits, unlocked) exactly up to its
// use cap and expiry; revoked, expired and paid-plan redemptions are refused.
func TestBetaInviteLifecycle(t *testing.T) {
	svc, st := scopeTestService(t)
	ctx := context.Background()
	now := time.Now()
	svc.clock = func() time.Time { return now }
	admin := Actor{UserID: scopeMakeUser(t, st, "beta-admin"), IsAdmin: true}

	if _, _, err := svc.CreateBetaInvite(ctx, Actor{UserID: uuid.New()}, "x", 1, 7); !errors.Is(err, ErrForbidden) {
		t.Fatalf("non-admin create: want ErrForbidden, got %v", err)
	}
	if _, _, err := svc.CreateBetaInvite(ctx, admin, "x", 1000, 7); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("oversized invite: want ErrInvalidInput, got %v", err)
	}
	inv, code, err := svc.CreateBetaInvite(ctx, admin, "for Sam", 2, 7)
	if err != nil || code == "" || !inv.Active || inv.MaxUses != 2 {
		t.Fatalf("create: inv=%+v code=%q err=%v", inv, code, err)
	}

	// A new account is locked until it redeems.
	a1, t1 := betaAccount(t, svc, "beta-one")
	if v, _ := svc.GetPersonalBilling(ctx, a1); v.Plan != PlanInactive || !v.Locked {
		t.Fatalf("before redeem = %+v", v)
	}
	if err := svc.RedeemBetaInvite(ctx, a1, "qb_wrong"); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("wrong code: want ErrInvalidInput, got %v", err)
	}
	if err := svc.RedeemBetaInvite(ctx, a1, " "+code+" "); err != nil {
		t.Fatalf("redeem: %v", err)
	}
	v, _ := svc.GetPersonalBilling(ctx, a1)
	if v.Plan != PlanBeta || v.Locked || v.Limits == nil || *v.Limits.StorageBytes != ProMaxStorageBytes {
		t.Fatalf("after redeem = %+v", v)
	}
	// Re-redeeming on the same account consumes nothing.
	if err := svc.RedeemBetaInvite(ctx, a1, code); err != nil {
		t.Fatalf("idempotent redeem: %v", err)
	}
	_ = t1

	// A paid account is never touched by a beta code.
	a2, t2 := betaAccount(t, svc, "beta-paid")
	if err := st.SetTenantPlan(ctx, db.SetTenantPlanParams{ID: t2.ID, Plan: PlanPro}); err != nil {
		t.Fatal(err)
	}
	if err := svc.RedeemBetaInvite(ctx, a2, code); !errors.Is(err, ErrConflict) {
		t.Fatalf("paid redeem: want ErrConflict, got %v", err)
	}

	// Second (last) use works, the third is refused.
	a3, _ := betaAccount(t, svc, "beta-three")
	if err := svc.RedeemBetaInvite(ctx, a3, code); err != nil {
		t.Fatalf("second use: %v", err)
	}
	a4, _ := betaAccount(t, svc, "beta-four")
	if err := svc.RedeemBetaInvite(ctx, a4, code); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("used-up code: want ErrInvalidInput, got %v", err)
	}

	// Expiry and revocation.
	_, code2, _ := svc.CreateBetaInvite(ctx, admin, "later", 5, 1)
	now = now.Add(25 * time.Hour)
	if err := svc.RedeemBetaInvite(ctx, a4, code2); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expired code: want ErrInvalidInput, got %v", err)
	}
	inv3, code3, _ := svc.CreateBetaInvite(ctx, admin, "revoked", 5, 7)
	if err := svc.RevokeBetaInvite(ctx, Actor{UserID: uuid.New()}, inv3.ID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("non-admin revoke: want ErrForbidden, got %v", err)
	}
	if err := svc.RevokeBetaInvite(ctx, admin, inv3.ID); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if err := svc.RedeemBetaInvite(ctx, a4, code3); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("revoked code: want ErrInvalidInput, got %v", err)
	}
	list, err := svc.ListBetaInvites(ctx, admin)
	if err != nil || len(list) != 3 {
		t.Fatalf("list = %d err=%v", len(list), err)
	}
}
