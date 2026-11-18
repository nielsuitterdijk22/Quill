package platform

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/nielsuitterdijk22/quill/internal/creem"
)

// fakeCreemSubs is a stateful stand-in for Creem's subscription API: one Team
// subscription whose single item carries the billed units. It records every
// seat update and can be switched to fail like an outage.
type fakeCreemSubs struct {
	mu      sync.Mutex
	units   map[string]int // subscription id -> units
	updates []string       // "sub:units:behavior"
	gets    int
	down    bool
}

func (f *fakeCreemSubs) handler(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.down {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
		return
	}
	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/v1/checkouts":
		fmt.Fprint(w, `{"id":"ch_1","checkout_url":"https://checkout.creem.io/ch_1"}`)
	case r.Method == http.MethodGet && r.URL.Path == "/v1/subscriptions":
		f.gets++
		id := r.URL.Query().Get("subscription_id")
		u, ok := f.units[id]
		if !ok {
			http.Error(w, `{"message":"not found"}`, http.StatusNotFound)
			return
		}
		fmt.Fprintf(w, `{"id":%q,"status":"active","items":[{"object":"subscription_item","id":"sitem_%s","product_id":"prod_team","units":%d}]}`, id, id, u)
	case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/v1/subscriptions/"):
		id := strings.TrimPrefix(r.URL.Path, "/v1/subscriptions/")
		var body struct {
			Items []struct {
				ID        string `json:"id"`
				ProductID string `json:"product_id"`
				Units     int    `json:"units"`
			} `json:"items"`
			UpdateBehavior string `json:"update_behavior"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if len(body.Items) != 1 || body.Items[0].ID != "sitem_"+id || body.Items[0].ProductID != "prod_team" {
			http.Error(w, `{"message":"Could not find product or price"}`, http.StatusNotFound)
			return
		}
		f.units[id] = body.Items[0].Units
		f.updates = append(f.updates, fmt.Sprintf("%s:%d:%s", id, body.Items[0].Units, body.UpdateBehavior))
		fmt.Fprintf(w, `{"id":%q,"status":"active"}`, id)
	default:
		http.NotFound(w, r)
	}
}

func (f *fakeCreemSubs) state() (map[string]int, []string, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	u := map[string]int{}
	for k, v := range f.units {
		u[k] = v
	}
	return u, append([]string(nil), f.updates...), f.gets
}

// TestTeamSeatsFollowMembership: the Creem subscription's seats track the
// org's member count — joining via an invite adds a seat, removal drops one —
// with proration charged on the next invoice. Trials never touch Creem. An
// outage never blocks a membership change, and the billing view heals the
// count once Creem is reachable again.
func TestTeamSeatsFollowMembership(t *testing.T) {
	svc, st := scopeTestService(t)
	ctx := context.Background()
	fake := &fakeCreemSubs{units: map[string]int{"sub_seats": 1}}
	srv := httptest.NewServer(http.HandlerFunc(fake.handler))
	t.Cleanup(srv.Close)
	svc.WithCreem(creem.New(creem.Config{APIKey: "sk_test", WebhookSecret: creemTestSecret, TeamProductID: "prod_team", BaseURL: srv.URL}))

	founder := Actor{UserID: scopeMakeUser(t, st, "seat-founder")}
	if _, _, err := svc.CreateOrganization(ctx, founder, "seatorg", "Seat Org"); err != nil {
		t.Fatal(err)
	}
	// Pre-subscription phase: a beta org is active but never touches Creem.
	activateOrg(t, svc, "seatorg", PlanBeta)
	join := func(name string) Actor {
		t.Helper()
		a := Actor{UserID: scopeMakeUser(t, st, name)}
		inv, err := svc.CreateInvite(ctx, founder, "seatorg", name+"@seat.test", "member")
		if err != nil {
			t.Fatalf("invite %s: %v", name, err)
		}
		if _, err := svc.AcceptInvite(ctx, a, inv.Token); err != nil {
			t.Fatalf("accept %s: %v", name, err)
		}
		return a
	}

	// Trial: membership changes never call Creem.
	trialMember := join("seat-trial")
	if _, ups, gets := fake.state(); len(ups) != 0 || gets != 0 {
		t.Fatalf("trial touched creem: updates=%v gets=%d", ups, gets)
	}
	if err := svc.RemoveOrgMember(ctx, founder, "seatorg", trialMember.UserID); err != nil {
		t.Fatal(err)
	}

	// Subscribe with one member: the webhook confirms 1 seat without an update.
	body := creemCheckoutCompleted("prod_team", "seatorg", "sub_seats")
	if _, err := svc.ProcessCreemWebhook(ctx, []byte(body), creemSig(body)); err != nil {
		t.Fatal(err)
	}
	if units, ups, _ := fake.state(); units["sub_seats"] != 1 || len(ups) != 0 {
		t.Fatalf("after checkout: units=%v updates=%v", units, ups)
	}
	if v, _ := svc.GetBilling(ctx, founder, "seatorg"); v.Seats != 1 || v.BilledSeats != 1 {
		t.Fatalf("billing after checkout: seats=%d billed=%d", v.Seats, v.BilledSeats)
	}

	// Two people join: 2, then 3 seats, prorated on the next invoice.
	alice := join("seat-alice")
	join("seat-bob")
	units, ups, _ := fake.state()
	if units["sub_seats"] != 3 || strings.Join(ups, ",") != "sub_seats:2:proration-charge,sub_seats:3:proration-charge" {
		t.Fatalf("after joins: units=%v updates=%v", units, ups)
	}

	// Removal drops a seat.
	if err := svc.RemoveOrgMember(ctx, founder, "seatorg", alice.UserID); err != nil {
		t.Fatal(err)
	}
	if units, _, _ := fake.state(); units["sub_seats"] != 2 {
		t.Fatalf("after removal: units=%v", units)
	}

	// Role changes don't change seats (no Creem round-trip at all).
	_, _, getsBefore := fake.state()
	bob, _ := st.GetUserByUsername(ctx, "seat-bob")
	if err := svc.UpdateOrgMemberRole(ctx, founder, "seatorg", bob.ID, "admin"); err != nil {
		t.Fatal(err)
	}
	if _, _, gets := fake.state(); gets != getsBefore {
		t.Fatalf("role change hit creem (%d -> %d gets)", getsBefore, gets)
	}

	// Outage: joining still works; Creem stays on the old count...
	fake.mu.Lock()
	fake.down = true
	fake.mu.Unlock()
	join("seat-carol")
	if units, _, _ := fake.state(); units["sub_seats"] != 2 {
		t.Fatalf("outage: units=%v", units)
	}
	// ...and the billing view heals it once Creem is back.
	fake.mu.Lock()
	fake.down = false
	fake.mu.Unlock()
	v, err := svc.GetBilling(ctx, founder, "seatorg")
	if err != nil {
		t.Fatal(err)
	}
	if units, _, _ := fake.state(); units["sub_seats"] != 3 || v.Seats != 3 || v.BilledSeats != 3 {
		t.Fatalf("after heal: units=%v seats=%d billed=%d", units, v.Seats, v.BilledSeats)
	}

	// Lapsed orgs aren't billed: an invite issued before the cancel can still
	// be accepted, but Creem is not asked to add a seat.
	inv, err := svc.CreateInvite(ctx, founder, "seatorg", "late@seat.test", "member")
	if err != nil {
		t.Fatal(err)
	}
	cancel := creemSubscriptionEvent("subscription.canceled", "prod_team", "sub_seats", `{"tenantSlug":"seatorg"}`)
	if _, err := svc.ProcessCreemWebhook(ctx, []byte(cancel), creemSig(cancel)); err != nil {
		t.Fatal(err)
	}
	_, upsBefore, _ := fake.state()
	late := Actor{UserID: scopeMakeUser(t, st, "seat-late")}
	if _, err := svc.AcceptInvite(ctx, late, inv.Token); err != nil {
		t.Fatalf("accept after lapse: %v", err)
	}
	if _, ups, _ := fake.state(); len(ups) != len(upsBefore) {
		t.Fatalf("lapsed org billed a seat: %v", ups)
	}
}
