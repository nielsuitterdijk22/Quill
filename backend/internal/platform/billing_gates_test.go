package platform

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nielsuitterdijk22/quill/internal/creem"
	"github.com/nielsuitterdijk22/quill/internal/store/db"
)

// Premium gates: the free plan carries hard limits (repos, pipeline minutes)
// enforced at the write paths; paid plans (team/enterprise/internal) are
// unlimited. These tests use a fresh database whose tenant plan default is
// 'free' (migration 000021), so a newly created org exercises the gates.

func TestCreateRepoEnforcesFreePlanLimit(t *testing.T) {
	svc, st := scopeTestService(t)
	ctx := context.Background()

	founder := Actor{UserID: scopeMakeUser(t, st, "gate-founder")}
	if _, _, err := svc.CreateOrganization(ctx, founder, "gateorg", "Gate Org"); err != nil {
		t.Fatalf("create org: %v", err)
	}
	// Orgs start on a Team trial; these tests exercise the free tier's caps.
	if _, err := svc.SetTenantPlan(ctx, Actor{IsAdmin: true}, "gateorg", PlanFree); err != nil {
		t.Fatalf("set free plan: %v", err)
	}
	project, err := st.GetProjectBySlug(ctx, "gateorg")
	if err != nil {
		t.Fatalf("get project: %v", err)
	}

	// Fill the quota with direct rows (no Forgejo round-trips needed).
	for i := 0; i < FreeMaxRepos; i++ {
		slug := "filler-" + string(rune('a'+i%26)) + "-" + string(rune('a'+i/26))
		if _, err := st.CreateRepository(ctx, db.CreateRepositoryParams{
			ProjectID: project.ID,
			Slug:      slug,
			Name:      slug,
		}); err != nil {
			t.Fatalf("seed filler repo %d: %v", i, err)
		}
	}

	// The next repo is rejected by the gate.
	if _, err := svc.CreateRepo(ctx, founder, "gateorg", CreateRepoInput{Slug: "over-limit"}); !errors.Is(err, ErrLimitReached) {
		t.Fatalf("over-quota create: want ErrLimitReached, got %v", err)
	}

	// Upgrading the tenant lifts the cap.
	if _, err := svc.SetTenantPlan(ctx, Actor{IsAdmin: true}, "gateorg", PlanTeam); err != nil {
		t.Fatalf("set plan: %v", err)
	}
	if _, err := svc.CreateRepo(ctx, founder, "gateorg", CreateRepoInput{Slug: "over-limit"}); err != nil {
		t.Fatalf("post-upgrade create: %v", err)
	}
}

// TestPipelineMinutesEnforceFreePlan gates workflow triggers once the tenant's
// finished-run minutes this calendar month reach the free cap.
func TestPipelineMinutesEnforceFreePlan(t *testing.T) {
	svc, st := scopeTestService(t)
	ctx := context.Background()

	founder := Actor{UserID: scopeMakeUser(t, st, "min-founder")}
	if _, _, err := svc.CreateOrganization(ctx, founder, "minorg", "Min Org"); err != nil {
		t.Fatalf("create org: %v", err)
	}
	// Orgs start on a Team trial; these tests exercise the free tier's caps.
	if _, err := svc.SetTenantPlan(ctx, Actor{IsAdmin: true}, "minorg", PlanFree); err != nil {
		t.Fatalf("set free plan: %v", err)
	}
	if _, err := svc.CreateRepo(ctx, founder, "minorg", CreateRepoInput{Slug: "ci", Name: "ci"}); err != nil {
		t.Fatalf("create repo: %v", err)
	}
	project, err := st.GetProjectBySlug(ctx, "minorg")
	if err != nil {
		t.Fatalf("get project: %v", err)
	}
	repo, err := st.GetRepositoryBySlug(ctx, db.GetRepositoryBySlugParams{ProjectID: project.ID, Lower: "ci"})
	if err != nil {
		t.Fatalf("get repo: %v", err)
	}

	// A finished run that consumed the entire monthly allowance.
	if _, err := st.Pool().Exec(ctx, `INSERT INTO pipelines (repo_id, workflow_path, name) VALUES ($1, '.quill/w.yml', 'w')`, repo.ID); err != nil {
		t.Fatalf("seed pipeline: %v", err)
	}
	if _, err := st.Pool().Exec(ctx, `INSERT INTO pipeline_runs (pipeline_id, run_number, status, event, ref, started_at, finished_at)
		VALUES ((SELECT id FROM pipelines WHERE repo_id = $1), 1, 'success', 'manual', 'main', now() - interval '101 minutes', now())`, repo.ID); err != nil {
		t.Fatalf("seed run: %v", err)
	}

	// The next trigger is rejected by the minutes gate (before any Forgejo call).
	if _, _, err := svc.TriggerRun(ctx, founder, "minorg", "ci", TriggerInput{WorkflowPath: ".quill/w.yml", Ref: "main"}); !errors.Is(err, ErrLimitReached) {
		t.Fatalf("over-quota trigger: want ErrLimitReached, got %v", err)
	}

	// Usage outside the current calendar month does not count against the gate.
	if _, err := st.Pool().Exec(ctx, `UPDATE pipeline_runs SET started_at = now() - interval '2 months', finished_at = now() - interval '2 months' + interval '101 minutes'`); err != nil {
		t.Fatalf("age out run: %v", err)
	}
	_, _, err = svc.TriggerRun(ctx, founder, "minorg", "ci", TriggerInput{WorkflowPath: ".quill/w.yml", Ref: "main"})
	if errors.Is(err, ErrLimitReached) {
		t.Fatalf("aged-out run still gated: %v", err)
	}
}

// TestCreateRepoEnforcesFreeStorageGate blocks repo creation once the tenant's
// metered storage reaches the free-plan allotment; the probe is swapped for a
// fake so no live Forgejo is needed.
func TestCreateRepoEnforcesFreeStorageGate(t *testing.T) {
	svc, st := scopeTestService(t)
	ctx := context.Background()

	founder := Actor{UserID: scopeMakeUser(t, st, "disk-founder")}
	if _, _, err := svc.CreateOrganization(ctx, founder, "diskorg", "Disk Org"); err != nil {
		t.Fatalf("create org: %v", err)
	}
	// Orgs start on a Team trial; these tests exercise the free tier's caps.
	if _, err := svc.SetTenantPlan(ctx, Actor{IsAdmin: true}, "diskorg", PlanFree); err != nil {
		t.Fatalf("set free plan: %v", err)
	}
	project, err := st.GetProjectBySlug(ctx, "diskorg")
	if err != nil {
		t.Fatalf("get project: %v", err)
	}

	// Force the probe to report a full disk: the next repo is rejected.
	svc.storageProbe = func(context.Context, uuid.UUID) (int64, error) {
		return int64(FreeMaxStorageBytes), nil
	}
	if _, err := svc.CreateRepo(ctx, founder, "diskorg", CreateRepoInput{Slug: "too-big"}); !errors.Is(err, ErrLimitReached) {
		t.Fatalf("over-storage create: want ErrLimitReached, got %v", err)
	}

	// Just under the limit passes.
	svc.storageProbe = func(context.Context, uuid.UUID) (int64, error) {
		return int64(FreeMaxStorageBytes) - 1, nil
	}
	if _, err := svc.CreateRepo(ctx, founder, "diskorg", CreateRepoInput{Slug: "just-fits"}); err != nil {
		t.Fatalf("under-limit create: %v", err)
	}

	// Paid plans are not storage-gated.
	if _, err := svc.SetTenantPlan(ctx, Actor{IsAdmin: true}, "diskorg", PlanTeam); err != nil {
		t.Fatalf("set plan: %v", err)
	}
	svc.storageProbe = func(context.Context, uuid.UUID) (int64, error) {
		return int64(FreeMaxStorageBytes) * 100, nil
	}
	if _, err := svc.CreateRepo(ctx, founder, "diskorg", CreateRepoInput{Slug: "team-is-fine"}); err != nil {
		t.Fatalf("team-plan create over storage: %v", err)
	}
	_ = project
}

// creemTestSecret signs webhook bodies the way Creem does: hex
// HMAC-SHA256(secret, raw body) in the creem-signature header.
const creemTestSecret = "whsec_test"

func creemSig(body string) string {
	mac := hmac.New(sha256.New, []byte(creemTestSecret))
	mac.Write([]byte(body))
	return hex.EncodeToString(mac.Sum(nil))
}

// creemCheckoutCompleted / creemSubscriptionEvent build payloads in Creem's
// documented shape (event data under "object", product/customer/subscription
// expanded on checkouts).
func creemCheckoutCompleted(product, tenantSlug, subID string) string {
	return fmt.Sprintf(`{"id":"evt_%[3]s","eventType":"checkout.completed","created_at":1728734325927,"object":{"id":"ch_%[3]s","object":"checkout","product":{"id":%[1]q,"object":"product"},"units":2,"customer":{"id":"cust_1","object":"customer","email":"a@x.test"},"subscription":{"id":%[3]q,"object":"subscription","status":"active"},"status":"completed","metadata":{"tenantSlug":%[2]q},"mode":"test"}}`, product, tenantSlug, subID)
}

func creemSubscriptionEvent(eventType, product, subID, metadataJSON string) string {
	return fmt.Sprintf(`{"id":"evt_%[2]s_%[1]s","eventType":%[1]q,"object":{"id":%[3]q,"object":"subscription","product":{"id":%[2]q,"object":"product"},"customer":{"id":"cust_1","object":"customer"},"status":"canceled","metadata":%[4]s}}`, eventType, product, subID, metadataJSON)
}

func configuredCreem(t *testing.T, svc *Service, gotBody *map[string]any) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if gotBody != nil {
			_ = json.NewDecoder(r.Body).Decode(gotBody)
		}
		fmt.Fprint(w, `{"id":"ch_1","checkout_url":"https://checkout.creem.io/ch_1"}`)
	}))
	t.Cleanup(srv.Close)
	svc.WithCreem(creem.New(creem.Config{
		APIKey:        "sk_test",
		WebhookSecret: creemTestSecret,
		TeamProductID: "prod_team",
		ProProductID:  "prod_pro",
		BaseURL:       srv.URL,
	}))
}

// TestNewOrgStartsTrialThenLocks: organizations are not free. A new org runs
// a TrialDays Team trial with no caps; once the window passes it is locked —
// repos, pipeline triggers and invites are refused with ErrLimitReached — and
// a paid Team plan unlocks it.
func TestNewOrgStartsTrialThenLocks(t *testing.T) {
	svc, st := scopeTestService(t)
	ctx := context.Background()
	now := time.Now()
	svc.clock = func() time.Time { return now }

	founder := Actor{UserID: scopeMakeUser(t, st, "trial-founder")}
	tenant, _, err := svc.CreateOrganization(ctx, founder, "trialorg", "Trial Org")
	if err != nil {
		t.Fatalf("create org: %v", err)
	}
	if tenant.Plan != PlanTrial {
		t.Fatalf("new org plan = %q, want %q", tenant.Plan, PlanTrial)
	}
	view, err := svc.GetBilling(ctx, founder, "trialorg")
	if err != nil {
		t.Fatal(err)
	}
	if view.Plan != PlanTrial || view.Locked || view.Limits != nil || view.TrialEndsAt == nil || view.Seats != 1 {
		t.Fatalf("trial view = %+v", view)
	}
	if d := view.TrialEndsAt.Sub(now); d < (TrialDays*24-1)*time.Hour || d > (TrialDays*24+1)*time.Hour {
		t.Fatalf("trial length = %v, want ~%d days", d, TrialDays)
	}

	// During the trial: everything works.
	if _, err := svc.CreateRepo(ctx, founder, "trialorg", CreateRepoInput{Slug: "during"}); err != nil {
		t.Fatalf("create repo during trial: %v", err)
	}
	if _, err := svc.CreateInvite(ctx, founder, "trialorg", "dev@trial.test", "member"); err != nil {
		t.Fatalf("invite during trial: %v", err)
	}

	// The org list (sidebar) tells members about the trial.
	if orgs, err := svc.ListOrganizations(ctx, founder); err != nil || len(orgs) != 1 || orgs[0].Plan != PlanTrial || orgs[0].TrialEndsAt == nil || orgs[0].Locked {
		t.Fatalf("org list during trial = %+v err=%v", orgs, err)
	}

	// After the window: locked.
	now = now.Add((TrialDays*24 + 1) * time.Hour)
	if orgs, _ := svc.ListOrganizations(ctx, founder); len(orgs) != 1 || !orgs[0].Locked {
		t.Fatalf("org list after trial = %+v, want locked", orgs)
	}
	view, _ = svc.GetBilling(ctx, founder, "trialorg")
	if !view.Locked {
		t.Fatalf("expired trial not locked: %+v", view)
	}
	if _, err := svc.CreateRepo(ctx, founder, "trialorg", CreateRepoInput{Slug: "after"}); !errors.Is(err, ErrLimitReached) {
		t.Fatalf("create repo after trial: want ErrLimitReached, got %v", err)
	}
	if _, err := svc.CreateInvite(ctx, founder, "trialorg", "late@trial.test", "member"); !errors.Is(err, ErrLimitReached) {
		t.Fatalf("invite after trial: want ErrLimitReached, got %v", err)
	}
	if _, _, err := svc.TriggerRun(ctx, founder, "trialorg", "during", TriggerInput{WorkflowPath: ".quill/w.yml", Ref: "main"}); !errors.Is(err, ErrLimitReached) {
		t.Fatalf("trigger after trial: want ErrLimitReached, got %v", err)
	}
	// Existing data stays readable.
	if _, err := svc.GetRepo(ctx, founder, "trialorg", "during"); err != nil {
		t.Fatalf("read repo of locked org: %v", err)
	}

	// Paying unlocks it.
	if _, err := svc.SetTenantPlan(ctx, Actor{IsAdmin: true}, "trialorg", PlanTeam); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateRepo(ctx, founder, "trialorg", CreateRepoInput{Slug: "after"}); err != nil {
		t.Fatalf("create repo after paying: %v", err)
	}
}

// TestTeamCheckoutFlow covers the Creem upgrade path at the platform layer:
// unconfigured payments answer ErrPaymentsNotConfigured; a configured one
// returns the hosted-checkout URL to tenant admins only, charging one unit per
// member; a correctly signed checkout.completed (Creem's real format) moves
// the org to team and records its subscription.
func TestTeamCheckoutFlow(t *testing.T) {
	svc, st := scopeTestService(t)
	ctx := context.Background()

	founder := Actor{UserID: scopeMakeUser(t, st, "pay-founder")}
	if _, _, err := svc.CreateOrganization(ctx, founder, "payorg", "Pay Org"); err != nil {
		t.Fatalf("create org: %v", err)
	}
	member := Actor{UserID: scopeMakeUser(t, st, "pay-member")}
	tenant, _ := st.GetTenantBySlug(ctx, "payorg")
	_ = st.AddTenantMember(ctx, db.AddTenantMemberParams{TenantID: tenant.ID, UserID: member.UserID, Role: "member"})

	// 1. Unconfigured: truthful error.
	if _, err := svc.StartTeamCheckout(ctx, founder, "payorg", "https://app/s", "req-1"); !errors.Is(err, ErrPaymentsNotConfigured) {
		t.Fatalf("unconfigured checkout: want ErrPaymentsNotConfigured, got %v", err)
	}
	if _, err := svc.ProcessCreemWebhook(ctx, []byte(`{}`), "x"); !errors.Is(err, ErrPaymentsNotConfigured) {
		t.Fatalf("unconfigured webhook: want ErrPaymentsNotConfigured, got %v", err)
	}

	// 2. Configured: checkout for admins, with seats = members.
	var sent map[string]any
	configuredCreem(t, svc, &sent)
	url, err := svc.StartTeamCheckout(ctx, founder, "payorg", "https://app/s", "req-1")
	if err != nil || url != "https://checkout.creem.io/ch_1" {
		t.Fatalf("checkout: url=%q err=%v", url, err)
	}
	if sent["product_id"] != "prod_team" || sent["units"] != float64(2) {
		t.Fatalf("checkout request = %v, want prod_team with 2 units", sent)
	}
	if md, _ := sent["metadata"].(map[string]any); md["tenantSlug"] != "payorg" {
		t.Fatalf("checkout metadata = %v", sent["metadata"])
	}
	if _, err := svc.StartTeamCheckout(ctx, member, "payorg", "https://app/s", "req-2"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("member checkout: want ErrForbidden, got %v", err)
	}

	// 3. Webhook: wrong signatures (including the old made-up format) are
	// rejected; Creem's real signature upgrades the plan.
	body := creemCheckoutCompleted("prod_team", "payorg", "sub_1")
	for _, bad := range []string{"", "00", "t=1,sha256=" + creemSig(body)} {
		if _, err := svc.ProcessCreemWebhook(ctx, []byte(body), bad); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("bad signature %q: want ErrInvalidInput, got %v", bad, err)
		}
	}
	res, err := svc.ProcessCreemWebhook(ctx, []byte(body), creemSig(body))
	if err != nil || res.TenantSlug != "payorg" || res.Plan != PlanTeam || !res.Upgraded {
		t.Fatalf("webhook: res=%+v err=%v", res, err)
	}
	view, err := svc.GetBilling(ctx, founder, "payorg")
	if err != nil || view.Plan != PlanTeam || view.Locked {
		t.Fatalf("billing after webhook: %+v err=%v", view, err)
	}
	sub, _ := st.GetTenantSubscription(ctx, tenant.ID)
	if sub.CreemSubscriptionID != "sub_1" || sub.CreemCustomerID != "cust_1" {
		t.Fatalf("creem ids not recorded: %+v", sub)
	}

	// Replays are idempotent (no second "upgraded" audit).
	res, err = svc.ProcessCreemWebhook(ctx, []byte(body), creemSig(body))
	if err != nil || res.Upgraded || res.Plan != PlanTeam {
		t.Fatalf("replay: res=%+v err=%v", res, err)
	}
	// A paid org can't open a second checkout.
	if _, err := svc.StartTeamCheckout(ctx, founder, "payorg", "https://app/s", "req-3"); !errors.Is(err, ErrConflict) {
		t.Fatalf("checkout on team plan: want ErrConflict, got %v", err)
	}

	// Unrelated events, unknown products and unknown tenants are no-ops.
	for _, b := range []string{
		`{"id":"evt_x","eventType":"refund.created","object":{"id":"ref_1","object":"refund"}}`,
		creemCheckoutCompleted("prod_unknown", "payorg", "sub_9"),
		creemCheckoutCompleted("prod_team", "no-such-org", "sub_9"),
	} {
		if r, err := svc.ProcessCreemWebhook(ctx, []byte(b), creemSig(b)); err != nil || r.TenantSlug != "" {
			t.Fatalf("ignored event %s: res=%+v err=%v", b, r, err)
		}
	}
	// Malformed JSON with a valid signature is an input error.
	if _, err := svc.ProcessCreemWebhook(ctx, []byte(`{nope`), creemSig(`{nope`)); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("malformed body: want ErrInvalidInput, got %v", err)
	}
}

// TestTeamSubscriptionLapse: a canceled or expired Team subscription locks
// the org (data kept), resolved by metadata or, when the event carries none,
// by the recorded subscription id. A cancel for a superseded subscription
// never revokes the current one, and resubscribing unlocks.
func TestTeamSubscriptionLapse(t *testing.T) {
	svc, st := scopeTestService(t)
	ctx := context.Background()
	configuredCreem(t, svc, nil)

	founder := Actor{UserID: scopeMakeUser(t, st, "lapse-founder")}
	if _, _, err := svc.CreateOrganization(ctx, founder, "lapseorg", "Lapse Org"); err != nil {
		t.Fatal(err)
	}
	apply := func(body string) CreemResult {
		t.Helper()
		res, err := svc.ProcessCreemWebhook(ctx, []byte(body), creemSig(body))
		if err != nil {
			t.Fatalf("webhook %s: %v", body, err)
		}
		return res
	}
	plan := func() string {
		t.Helper()
		v, err := svc.GetBilling(ctx, founder, "lapseorg")
		if err != nil {
			t.Fatal(err)
		}
		return v.Plan
	}

	apply(creemCheckoutCompleted("prod_team", "lapseorg", "sub_old"))
	if _, err := svc.CreateRepo(ctx, founder, "lapseorg", CreateRepoInput{Slug: "kept"}); err != nil {
		t.Fatal(err)
	}

	// subscription.expired without metadata (as in Creem's sample) resolves
	// through the recorded subscription id and locks the org.
	if r := apply(creemSubscriptionEvent("subscription.expired", "prod_team", "sub_old", `{}`)); r.TenantSlug != "lapseorg" || r.Plan != PlanLapsed || r.Upgraded {
		t.Fatalf("expired: %+v", r)
	}
	if plan() != PlanLapsed {
		t.Fatalf("plan after expiry = %q", plan())
	}
	if _, err := svc.CreateRepo(ctx, founder, "lapseorg", CreateRepoInput{Slug: "blocked"}); !errors.Is(err, ErrLimitReached) {
		t.Fatalf("create repo when lapsed: want ErrLimitReached, got %v", err)
	}
	if _, err := svc.GetRepo(ctx, founder, "lapseorg", "kept"); err != nil {
		t.Fatalf("lapsed org data must stay readable: %v", err)
	}
	if v, _ := svc.GetBilling(ctx, founder, "lapseorg"); !v.Locked {
		t.Fatalf("lapsed org not reported locked: %+v", v)
	}

	// Resubscribe through a fresh checkout (new subscription id): unlocked.
	if _, err := svc.StartTeamCheckout(ctx, founder, "lapseorg", "https://app/s", "req"); err != nil {
		t.Fatalf("checkout from lapsed: %v", err)
	}
	apply(creemCheckoutCompleted("prod_team", "lapseorg", "sub_new"))
	if plan() != PlanTeam {
		t.Fatalf("plan after resubscribe = %q", plan())
	}

	// A late cancel of the OLD subscription must not revoke the new one.
	if r := apply(creemSubscriptionEvent("subscription.canceled", "prod_team", "sub_old", `{"tenantSlug":"lapseorg"}`)); r.TenantSlug != "" {
		t.Fatalf("stale cancel acted: %+v", r)
	}
	if plan() != PlanTeam {
		t.Fatalf("stale cancel changed plan to %q", plan())
	}
	// Cancel of the current one lapses it.
	apply(creemSubscriptionEvent("subscription.canceled", "prod_team", "sub_new", `{"tenantSlug":"lapseorg"}`))
	if plan() != PlanLapsed {
		t.Fatalf("plan after cancel = %q", plan())
	}

	// subscription.paid (renewal) re-grants Team idempotently.
	apply(creemSubscriptionEvent("subscription.paid", "prod_team", "sub_new", `{"tenantSlug":"lapseorg"}`))
	if plan() != PlanTeam {
		t.Fatalf("plan after paid = %q", plan())
	}

	// Vendor-provisioned plans are never touched by self-service events.
	if _, err := svc.SetTenantPlan(ctx, Actor{IsAdmin: true}, "lapseorg", PlanEnterprise); err != nil {
		t.Fatal(err)
	}
	apply(creemSubscriptionEvent("subscription.canceled", "prod_team", "sub_new", `{"tenantSlug":"lapseorg"}`))
	apply(creemCheckoutCompleted("prod_team", "lapseorg", "sub_new"))
	if plan() != PlanEnterprise {
		t.Fatalf("enterprise plan overwritten: %q", plan())
	}
}

// TestPersonalBillingAndProUpgrade covers the individual (non-org) freemium
// surfaces: the account's billing view reads its own per-account tenant, the
// Pro checkout resolves to that same tenant, a verified pro webhook flips the
// plan to pro (lifting the caps), and cancelling returns it to free — never
// to a locked state (individuals keep the free tier).
func TestPersonalBillingAndProUpgrade(t *testing.T) {
	svc, st := scopeTestService(t)
	ctx := context.Background()

	userID := scopeMakeUser(t, st, "pro-user")
	// Provision the per-account tenant the way a fresh Zitadel first login
	// does (auth/zitadel.go): tenant slug = username, user row linked.
	tenant, err := st.CreateTenant(ctx, db.CreateTenantParams{Slug: "pro-user", Name: "pro-user"})
	if err != nil {
		t.Fatalf("create account tenant: %v", err)
	}
	if _, err := st.SetUserTenant(ctx, db.SetUserTenantParams{
		ID:       userID,
		TenantID: uuid.NullUUID{UUID: tenant.ID, Valid: true},
	}); err != nil {
		t.Fatalf("link account tenant: %v", err)
	}
	if err := svc.CreatePersonalProject(ctx, userID, "pro-user"); err != nil {
		t.Fatalf("personal project: %v", err)
	}

	// The personal billing view reports the account tenant: free plan, caps set.
	view, err := svc.GetPersonalBilling(ctx, Actor{UserID: userID})
	if err != nil {
		t.Fatalf("GetPersonalBilling: %v", err)
	}
	if view.Plan != PlanFree || view.Locked || view.Limits == nil || view.Limits.Repos == nil || *view.Limits.Repos != FreeMaxRepos {
		t.Fatalf("personal view = %+v, want free with caps", view)
	}

	// Unconfigured payments: the personal checkout is truthful.
	if _, err := svc.StartPersonalCheckout(ctx, Actor{UserID: userID}, "https://app/s", "req-1"); !errors.Is(err, ErrPaymentsNotConfigured) {
		t.Fatalf("unconfigured personal checkout: want ErrPaymentsNotConfigured, got %v", err)
	}

	var sent map[string]any
	configuredCreem(t, svc, &sent)
	url, err := svc.StartPersonalCheckout(ctx, Actor{UserID: userID}, "https://app/s", "req-2")
	if err != nil || url != "https://checkout.creem.io/ch_1" {
		t.Fatalf("personal checkout: url=%q err=%v", url, err)
	}
	if sent["product_id"] != "prod_pro" || sent["units"] != float64(1) {
		t.Fatalf("personal checkout request = %v", sent)
	}

	// Legacy accounts (no per-account tenant) get the clear error.
	legacy := scopeMakeUser(t, st, "legacy-user")
	if _, err := svc.StartPersonalCheckout(ctx, Actor{UserID: legacy}, "https://app/s", "req-3"); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("legacy checkout: want ErrInvalidInput, got %v", err)
	}

	// A verified pro checkout.completed webhook flips the plan to Pro.
	body := creemCheckoutCompleted("prod_pro", "pro-user", "sub_pro")
	res, err := svc.ProcessCreemWebhook(ctx, []byte(body), creemSig(body))
	if err != nil || res.TenantSlug != "pro-user" || res.Plan != PlanPro {
		t.Fatalf("pro webhook: res=%+v err=%v", res, err)
	}
	view, err = svc.GetPersonalBilling(ctx, Actor{UserID: userID})
	if err != nil {
		t.Fatalf("billing after webhook: %v", err)
	}
	if view.Plan != PlanPro || view.Limits != nil {
		t.Fatalf("plan after pro webhook = %q limits=%+v, want pro/unlimited", view.Plan, view.Limits)
	}

	// Cancelling Pro returns the account to the free tier (caps, not a lock).
	cancel := creemSubscriptionEvent("subscription.canceled", "prod_pro", "sub_pro", `{"tenantSlug":"pro-user"}`)
	if _, err := svc.ProcessCreemWebhook(ctx, []byte(cancel), creemSig(cancel)); err != nil {
		t.Fatal(err)
	}
	view, _ = svc.GetPersonalBilling(ctx, Actor{UserID: userID})
	if view.Plan != PlanFree || view.Locked || view.Limits == nil {
		t.Fatalf("after pro cancel = %+v, want free with caps", view)
	}
}
