package platform_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/nielsuitterdijk22/quill/internal/platform"
	"github.com/nielsuitterdijk22/quill/internal/store/db"
)

// TestCIWorkerRepoIsolation: an isolated CI instance (token worker) serves
// one repository. Its first claim binds it; afterwards it only ever receives
// that repo's jobs. A second worker never gets a repo a live worker serves;
// a retired worker claims nothing more. The broker is told how many repos
// still wait for an instance of their own.
func TestCIWorkerRepoIsolation(t *testing.T) {
	svc, st := newService(t)
	svc = svc.WithQueueMode()
	ctx := context.Background()
	// Leftovers from other tests in the shared DB would be claimable too.
	for {
		j, err := svc.ClaimQueuedJob(ctx, "drain-"+uuid.NewString())
		if err != nil {
			t.Fatal(err)
		}
		if j == nil {
			break
		}
	}
	repoA := makeProjectWithRepo(t, st, "iso-a-"+uuid.NewString()[:6])
	repoB := makeProjectWithRepo(t, st, "iso-b-"+uuid.NewString()[:6])
	enqueue := func(r db.Repository, path string) uuid.UUID {
		t.Helper()
		run, err := svc.EnqueuePipelineRun(ctx, r, platform.TriggerInput{WorkflowPath: path, Ref: "main", Event: "manual"}, queueWorkflowYAML, "deadbeef", uuid.NullUUID{})
		if err != nil {
			t.Fatalf("enqueue: %v", err)
		}
		return run.ID
	}
	a1 := enqueue(repoA, ".github/workflows/a1.yml")
	b1 := enqueue(repoB, ".github/workflows/b1.yml")
	a2 := enqueue(repoA, ".github/workflows/a2.yml")

	if n, err := st.CIReposNeedingWorker(ctx, 0); err != nil || n != 2 {
		t.Fatalf("repos needing a worker = %d, %v (want 2)", n, err)
	}

	w1, w2 := "ciw-"+uuid.NewString(), "ciw-"+uuid.NewString()
	j, err := svc.ClaimQueuedJobScoped(ctx, w1)
	if err != nil || j == nil || j.RunID != a1 {
		t.Fatalf("w1 first claim: %+v %v (want run a1)", j, err)
	}
	if n, _ := st.CIReposNeedingWorker(ctx, 0); n != 1 {
		t.Fatalf("after w1 bound to A, repos needing a worker = %d (want 1)", n)
	}
	// w2 is unbound: repo A has a live worker, so it gets B.
	j, err = svc.ClaimQueuedJobScoped(ctx, w2)
	if err != nil || j == nil || j.RunID != b1 {
		t.Fatalf("w2 claim: %+v %v (want run b1)", j, err)
	}
	// w2 is now bound to B: A's pending a2 is not its to take.
	if j, err := svc.ClaimQueuedJobScoped(ctx, w2); err != nil || j != nil {
		t.Fatalf("w2 got another repo's job: %+v %v", j, err)
	}
	// w1 gets A's next job.
	if j, err := svc.ClaimQueuedJobScoped(ctx, w1); err != nil || j == nil || j.RunID != a2 {
		t.Fatalf("w1 second claim: %+v %v (want run a2)", j, err)
	}
	if n, _ := st.CIReposNeedingWorker(ctx, 0); n != 0 {
		t.Fatalf("all claimed, repos needing a worker = %d", n)
	}

	// A retired worker never claims again, even with work for its repo.
	a3 := enqueue(repoA, ".github/workflows/a3.yml")
	if err := svc.RetireCIWorker(ctx, w1, ""); err != nil {
		t.Fatal(err)
	}
	if j, err := svc.ClaimQueuedJobScoped(ctx, w1); err != nil || j != nil {
		t.Fatalf("retired worker claimed: %+v %v", j, err)
	}
	// ...and repo A now needs a fresh instance, which gets it.
	if n, _ := st.CIReposNeedingWorker(ctx, 0); n != 1 {
		t.Fatalf("after retire, repos needing a worker = %d (want 1)", n)
	}
	w3 := "ciw-" + uuid.NewString()
	if j, err := svc.ClaimQueuedJobScoped(ctx, w3); err != nil || j == nil || j.RunID != a3 {
		t.Fatalf("w3 claim: %+v %v (want run a3)", j, err)
	}

	// Same repository, but a run drawing a deployment environment's secrets
	// is a different scope: w3 (serving A's plain runs) never gets it — a
	// pull-request run that broke out of its container must not be able to
	// claim the production deploy that follows.
	if _, err := st.CreateEnvironment(ctx, db.CreateEnvironmentParams{ProjectID: repoA.ProjectID, Slug: "production", Name: "production"}); err != nil {
		t.Fatalf("environment: %v", err)
	}
	prod, err := svc.EnqueuePipelineRun(ctx, repoA, platform.TriggerInput{
		WorkflowPath: ".github/workflows/deploy.yml", Ref: "main", Event: "manual", EnvironmentSlug: "production",
	}, queueWorkflowYAML, "deadbeef", uuid.NullUUID{})
	if err != nil {
		t.Fatalf("enqueue prod: %v", err)
	}
	if j, err := svc.ClaimQueuedJobScoped(ctx, w3); err != nil || j != nil {
		t.Fatalf("w3 got the production-environment run of its repo: %+v %v", j, err)
	}
	w4 := "ciw-" + uuid.NewString()
	if j, err := svc.ClaimQueuedJobScoped(ctx, w4); err != nil || j == nil || j.RunID != prod.ID {
		t.Fatalf("w4 claim: %+v %v (want the production run)", j, err)
	}
}
