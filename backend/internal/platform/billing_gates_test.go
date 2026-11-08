package platform

import (
	"context"
	"errors"
	"testing"

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
