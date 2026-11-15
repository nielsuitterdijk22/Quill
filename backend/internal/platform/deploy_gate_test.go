package platform_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nielsuitterdijk22/quill/internal/pipeline"
	"github.com/nielsuitterdijk22/quill/internal/platform"
	"github.com/nielsuitterdijk22/quill/internal/store/db"
)

const deployWorkflowYAML = `
name: deploy
on: [push]
jobs:
  deploy:
    runs-on: ubuntu-latest
    environment: production
    steps:
      - run: echo deploy
`

// Environment policies gate runs (2026-11-15; before, they were stored and
// never evaluated). A workflow's `environment:` targets an environment; the
// policies of repo/project/org decide before anything is queued; a refused
// run is failed with its reasons, never reaches a worker, and the run page
// says why.
func TestDeployGate(t *testing.T) {
	svc, st := newService(t)
	svc = svc.WithQueueMode()
	ctx := context.Background()
	repo := makeProjectWithRepo(t, st, "gate")
	project, err := st.GetProjectByID(ctx, repo.ProjectID)
	if err != nil {
		t.Fatal(err)
	}
	admin := platform.Actor{UserID: makeUser(t, st, "gate-admin"), IsAdmin: true}

	enqueue := func(yaml, ref, sha string) db.PipelineRun {
		t.Helper()
		run, err := svc.EnqueuePipelineRun(ctx, repo, platform.TriggerInput{WorkflowPath: ".github/workflows/deploy.yml", Ref: ref, Event: "push"},
			yaml, sha, uuid.NullUUID{})
		if err != nil {
			t.Fatalf("enqueue: %v", err)
		}
		return run
	}
	detail := func(run db.PipelineRun) platform.RunDetail {
		t.Helper()
		_, d, err := svc.GetRun(ctx, admin, project.Slug, repo.Slug, ".github/workflows/deploy.yml", int(run.RunNumber))
		if err != nil {
			t.Fatalf("GetRun: %v", err)
		}
		return d
	}
	blocked := func(run db.PipelineRun, want string) {
		t.Helper()
		d := detail(run)
		if d.Run.Status != pipeline.StatusFailure || !strings.Contains(d.BlockedReason, want) {
			t.Fatalf("run %d: status=%s blocked=%q, want failure containing %q", run.RunNumber, d.Run.Status, d.BlockedReason, want)
		}
		if j, _ := svc.ClaimQueuedJob(ctx, "gate-worker"); j != nil && j.RunID == run.ID {
			t.Fatal("a blocked run reached the queue")
		}
	}
	allowed := func(run db.PipelineRun) {
		t.Helper()
		d := detail(run)
		if d.BlockedReason != "" || d.Run.Status != pipeline.StatusPending || d.Environment != "production" {
			t.Fatalf("run %d should be queued for production: %+v", run.RunNumber, d)
		}
		for i := 0; i < 50; i++ {
			j, err := svc.ClaimQueuedJob(ctx, "gate-worker")
			if err != nil || j == nil {
				t.Fatalf("claim: %v %v", j, err)
			}
			if j.RunID == run.ID {
				if j.Spec.Secrets["PROD_ONLY"] != "p" {
					t.Fatal("deploy run lacks the environment's secret")
				}
				return
			}
		}
		t.Fatal("allowed run not queued")
	}

	// An environment the project doesn't define blocks the run.
	blocked(enqueue(deployWorkflowYAML, "main", "c1"), "not defined")

	if _, err := svc.CreateEnvironment(ctx, admin, project.Slug, platform.CreateEnvironmentInput{Slug: "production", Name: "Production"}); err != nil {
		t.Fatalf("create env: %v", err)
	}
	if _, err := svc.SetEnvironmentSecret(ctx, admin, project.Slug, "production", "PROD_ONLY", "p"); err != nil {
		t.Fatalf("env secret: %v", err)
	}
	// No policy: the environment alone gates nothing.
	allowed(enqueue(deployWorkflowYAML, "feature", "c1"))

	// Only main, and only a commit with a green run, may deploy.
	if _, err := svc.SetProjectEnvironmentPolicy(ctx, admin, project.Slug, platform.EnvironmentPolicyInput{
		Selector: "production", AllowedSourceBranches: []string{"main"}, RequireSuccessfulRun: true,
	}); err != nil {
		t.Fatalf("policy: %v", err)
	}
	blocked(enqueue(deployWorkflowYAML, "refs/heads/feature", "c2"), `ref "feature" may not deploy`)
	blocked(enqueue(deployWorkflowYAML, "refs/heads/main", "c2"), "no successful pipeline run")

	ci := enqueue(queueWorkflowYAML, "main", "c2") // a non-deploy run of the commit
	if _, err := st.UpdatePipelineRunStatus(ctx, db.UpdatePipelineRunStatusParams{ID: ci.ID, Status: pipeline.StatusSuccess, FinishedAt: pgtype.Timestamptz{}}); err != nil {
		t.Fatal(err)
	}
	allowed(enqueue(deployWorkflowYAML, "refs/heads/main", "c2"))

	// Approvals have no approval flow yet: requiring them blocks, visibly.
	if _, err := svc.SetEnvironmentPolicy(ctx, admin, project.Slug, repo.Slug, platform.EnvironmentPolicyInput{
		Selector: "production", RequiredApprovals: 1,
	}); err != nil {
		t.Fatalf("repo policy: %v", err)
	}
	blocked(enqueue(deployWorkflowYAML, "main", "c2"), "required approvals")

	// Explicitly requesting an unknown environment is a client error.
	if _, err := svc.EnqueuePipelineRun(ctx, repo, platform.TriggerInput{WorkflowPath: "x.yml", Ref: "main", EnvironmentSlug: "nope"},
		queueWorkflowYAML, "c3", uuid.NullUUID{}); !errors.Is(err, platform.ErrInvalidInput) {
		t.Fatalf("unknown explicit environment: %v", err)
	}
}
