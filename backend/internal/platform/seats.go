package platform

import (
	"context"
	"fmt"
	"sync"

	"github.com/google/uuid"

	"github.com/nielsuitterdijk22/quill/internal/creem"
)

// Team is billed per seat: the Creem subscription's units follow the org's
// member count. syncTeamSeats reconciles the two and is called after every
// membership change, on paid-plan webhooks, and from the billing view (so a
// sync that failed while Creem was unreachable heals on the next touch).
//
// Membership changes never fail because of billing: callers use
// syncTeamSeatsBestEffort, which logs instead of returning the error.

// seatLock serializes syncs per tenant within this instance, so two
// concurrent membership changes can't leave Creem on the older count.
func (s *Service) seatLock(tenantID uuid.UUID) *sync.Mutex {
	m, _ := s.seatLocks.LoadOrStore(tenantID, &sync.Mutex{})
	return m.(*sync.Mutex)
}

// syncTeamSeats sets the Creem subscription's seats to the org's member count.
// A no-op unless the tenant is on the Team plan with a recorded subscription,
// and when the count last confirmed on Creem already matches.
func (s *Service) syncTeamSeats(ctx context.Context, tenantID uuid.UUID) error {
	if s.creem == nil || !s.creem.Enabled() {
		return nil
	}
	mu := s.seatLock(tenantID)
	mu.Lock()
	defer mu.Unlock()

	tenant, err := s.store.GetTenantByID(ctx, tenantID)
	if err != nil {
		return fmt.Errorf("lookup tenant: %w", err)
	}
	if tenant.Plan != PlanTeam {
		return nil
	}
	sub, err := s.store.GetTenantSubscription(ctx, tenantID)
	if err != nil {
		return fmt.Errorf("load subscription: %w", err)
	}
	if sub.CreemSubscriptionID == "" {
		return nil // provisioned by hand (admin set-plan), nothing to bill
	}
	members, err := s.store.ListTenantMembers(ctx, tenantID)
	if err != nil {
		return fmt.Errorf("count members: %w", err)
	}
	want := max(1, len(members))
	if sub.CreemSeats == want {
		return nil
	}

	cs, err := s.creem.GetSubscription(ctx, sub.CreemSubscriptionID)
	if err != nil {
		return fmt.Errorf("get creem subscription: %w", err)
	}
	item, ok := teamItem(cs, s.creem.TeamProductID())
	if !ok {
		return fmt.Errorf("creem subscription %s has no Team item", sub.CreemSubscriptionID)
	}
	if item.Units != want {
		if err := s.creem.UpdateSubscriptionUnits(ctx, cs.ID, item, want); err != nil {
			return fmt.Errorf("update creem seats: %w", err)
		}
		s.logger.Info("creem seats updated", "tenant", tenant.Slug, "from", item.Units, "to", want, "subscription", cs.ID)
	}
	if err := s.store.SetTenantCreemSeats(ctx, tenantID, want); err != nil {
		return fmt.Errorf("record seats: %w", err)
	}
	return nil
}

// syncTeamSeatsBestEffort runs syncTeamSeats on a context detached from the
// caller's request (a client disconnect must not strand a half-done sync) and
// logs failures; the next membership change or billing view retries.
func (s *Service) syncTeamSeatsBestEffort(ctx context.Context, tenantID uuid.UUID) {
	cctx, cancel := detachedContext(ctx)
	defer cancel()
	if err := s.syncTeamSeats(cctx, tenantID); err != nil {
		s.logger.Warn("creem seat sync failed; will retry on the next membership change or billing view", "tenant", tenantID, "error", err)
	}
}

// teamItem picks the subscription line that sells the Team product (or the
// only line, when Creem omits product ids).
func teamItem(sub *creem.Subscription, teamProductID string) (creem.SubscriptionItem, bool) {
	for _, it := range sub.Items {
		if it.ProductID == teamProductID {
			return it, true
		}
	}
	if len(sub.Items) == 1 && sub.Items[0].ProductID == "" {
		return sub.Items[0], true
	}
	return creem.SubscriptionItem{}, false
}
