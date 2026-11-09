package platform

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
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

// TestTeamCheckoutFlow covers the Creem upgrade path end-to-end at the platform
// layer: an unconfigured deployment answers ErrPaymentsNotConfigured, a
// configured one returns the hosted-checkout URL for a tenant admin only, and
// a verified checkout.completed webhook flips the plan to team.
func TestTeamCheckoutFlow(t *testing.T) {
	svc, st := scopeTestService(t)
	ctx := context.Background()

	founder := Actor{UserID: scopeMakeUser(t, st, "pay-founder")}
	if _, _, err := svc.CreateOrganization(ctx, founder, "payorg", "Pay Org"); err != nil {
		t.Fatalf("create org: %v", err)
	}

	// 1. Unconfigured: truthful error.
	if _, err := svc.StartTeamCheckout(ctx, founder, "payorg", "https://app/s", "req-1"); !errors.Is(err, ErrPaymentsNotConfigured) {
		t.Fatalf("unconfigured checkout: want ErrPaymentsNotConfigured, got %v", err)
	}

	// 2. Configured client against a fake Creem.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"id":"ch_1","checkout_url":"https://pay.creem.io/s/session-1"}`)
	}))
	defer srv.Close()
	svc.WithCreem(creem.New(creem.Config{
		APIKey:        "sk_test",
		WebhookSecret: "whsec_test",
		TeamProductID: "prod_team",
		BaseURL:       srv.URL,
	}))

	url, err := svc.StartTeamCheckout(ctx, founder, "payorg", "https://app/s", "req-1")
	if err != nil || url != "https://pay.creem.io/s/session-1" {
		t.Fatalf("checkout: url=%q err=%v", url, err)
	}

	// Non-admin members may not start checkouts.
	member := Actor{UserID: scopeMakeUser(t, st, "pay-member")}
	tenant, _ := st.GetTenantBySlug(ctx, "payorg")
	_ = st.AddTenantMember(ctx, db.AddTenantMemberParams{TenantID: tenant.ID, UserID: member.UserID, Role: "member"})
	if _, err := svc.StartTeamCheckout(ctx, member, "payorg", "https://app/s", "req-2"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("member checkout: want ErrForbidden, got %v", err)
	}

	// 3. Webhook: bad signature rejected, good signature upgrades the plan.
	body := []byte(`{"eventType":"checkout.completed","obj":{"id":"ch_1","product_id":"prod_team","customer_id":"cus_1","metadata":{"orgSlug":"payorg"}}}`)
	ts := time.Now().Unix()
	mac := hmac.New(sha256.New, []byte("whsec_test"))
	mac.Write([]byte(fmt.Sprintf("%d.", ts)))
	mac.Write(body)
	sig := fmt.Sprintf("t=%d,sha256=%s", ts, hex.EncodeToString(mac.Sum(nil)))

	if _, err := svc.ProcessCreemWebhook(ctx, body, "t=1,sha256=00"); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("bad signature: want ErrInvalidInput, got %v", err)
	}
	orgSlug, err := svc.ProcessCreemWebhook(ctx, body, sig)
	if err != nil || orgSlug != "payorg" {
		t.Fatalf("webhook: orgSlug=%q err=%v", orgSlug, err)
	}
	view, err := svc.GetBilling(ctx, founder, "payorg")
	if err != nil {
		t.Fatalf("billing after webhook: %v", err)
	}
	if view.Plan != PlanTeam {
		t.Fatalf("plan after webhook = %q, want %q", view.Plan, PlanTeam)
	}

	// Unrelated events are ignored (200-style no-op).
	other := []byte(`{"eventType":"subscription.canceled"}`)
	osM := hmac.New(sha256.New, []byte("whsec_test"))
	osM.Write([]byte(fmt.Sprintf("%d.", ts)))
	osM.Write(other)
	if slug, err := svc.ProcessCreemWebhook(ctx, other, fmt.Sprintf("t=%d,sha256=%s", ts, hex.EncodeToString(osM.Sum(nil)))); err != nil || slug != "" {
		t.Fatalf("unrelated event: slug=%q err=%v", slug, err)
	}
}
