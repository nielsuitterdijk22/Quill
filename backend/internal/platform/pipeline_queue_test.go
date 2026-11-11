package platform_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nielsuitterdijk22/quill/internal/pipeline"
	"github.com/nielsuitterdijk22/quill/internal/platform"
	"github.com/nielsuitterdijk22/quill/internal/store"
	"github.com/nielsuitterdijk22/quill/internal/store/db"
)

// makeProjectWithRepo seeds a tenant + project + repository row directly
// through the store — no Forgejo involved.
func makeProjectWithRepo(t *testing.T, st *store.Store, name string) db.Repository {
	t.Helper()
	ctx := context.Background()
	tenant, err := st.GetOrCreateTenantByExternalOrg(ctx, uuid.NewString(), "qt-"+name)
	if err != nil {
		t.Fatalf("create tenant: %v", err)
	}
	project, err := st.CreateProject(ctx, db.CreateProjectParams{
		TenantID: tenant.ID, Slug: name, Name: name,
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	repo, err := st.CreateRepository(ctx, db.CreateRepositoryParams{
		ProjectID: project.ID, Slug: name, Name: name,
		Visibility: "private", DefaultBranch: "main",
	})
	if err != nil {
		t.Fatalf("create repository: %v", err)
	}
	return repo
}

// queueWorkflowYAML is the one-job workflow used by the queue fixtures.
const queueWorkflowYAML = `
name: test
on: [push]
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: echo hello
`

// TestQueueModeEnqueueThenClaim verifies the enqueue/claim lifecycle: a
// queued trigger persists the run as pending with a queue row stamped with
// the tenant, claiming it flips the run to running, decodes the full JobSpec,
// and a second claim finds no work.
func TestQueueModeEnqueueThenClaim(t *testing.T) {
	svc, st := newService(t)
	svc = svc.WithQueueMode()
	ctx := context.Background()
	repo := makeProjectWithRepo(t, st, "q1")

	run, err := svc.EnqueuePipelineRun(ctx, repo, platform.TriggerInput{
		WorkflowPath: ".github/workflows/test.yml", Ref: "main", Event: "manual",
	}, queueWorkflowYAML, "deadbeef", uuid.NullUUID{})
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if run.Status != pipeline.StatusPending {
		t.Fatalf("queued run should be pending, got %q", run.Status)
	}

	// No queue mode, no claim: the seam only claims in queue mode.
	job, err := svc.ClaimQueuedJob(ctx, "worker-a")
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if job == nil {
		t.Fatal("expected a claimable job, got none")
	}
	if job.RunID != run.ID {
		t.Fatalf("claim returned run %v, want %v", job.RunID, run.ID)
	}
	if job.Spec.WorkflowYAML != queueWorkflowYAML || job.Spec.Ref != "main" || job.Spec.Event != "manual" {
		t.Fatalf("spec round-trip mismatch: %+v", job.Spec)
	}
	if job.Spec.RepoFullName == "" {
		t.Fatal("spec should carry the repo full name for act's github expressions")
	}
	if !job.TenantID.Valid {
		t.Fatal("queue job must carry the tenant id for metering")
	}

	// Run flipped to running by the claim.
	got, err := st.GetPipelineRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("reload run: %v", err)
	}
	if got.Status != pipeline.StatusRunning {
		t.Fatalf("claimed run should be running, got %q", got.Status)
	}

	// The queue is drained: no second claim.
	again, err := svc.ClaimQueuedJob(ctx, "worker-b")
	if err != nil {
		t.Fatalf("second claim: %v", err)
	}
	if again != nil {
		t.Fatal("second claim should find no work while the job is claimed")
	}
}

// TestQueueClaimExclusiveUnderConcurrency hammers the claim path from several
// goroutines: every enqueued job must be claimed exactly once.
func TestQueueClaimExclusiveUnderConcurrency(t *testing.T) {
	svc, st := newService(t)
	svc = svc.WithQueueMode()
	ctx := context.Background()
	repo := makeProjectWithRepo(t, st, "q2")

	const jobs = 8
	for i := 0; i < jobs; i++ {
		if _, err := svc.EnqueuePipelineRun(ctx, repo, platform.TriggerInput{
			WorkflowPath: ".github/workflows/one.yml", Ref: "main", Event: "push",
		}, queueWorkflowYAML, "deadbeef", uuid.NullUUID{}); err != nil {
			t.Fatalf("enqueue %d: %v", i, err)
		}
	}

	const workers = 6
	claimed := make(chan string, jobs)
	var joinDone = make(chan struct{})
	go func() {
		seen := make(map[string]bool, jobs)
		for id := range claimed {
			if seen[id] {
				t.Errorf("job %s claimed twice", id)
				close(joinDone)
				return
			}
			seen[id] = true
			if len(seen) == jobs {
				close(joinDone)
				return
			}
		}
	}()

	claim := func(worker string) {
		for {
			job, err := svc.ClaimQueuedJob(ctx, worker)
			if err != nil {
				t.Errorf("claim %s: %v", worker, err)
				return
			}
			if job == nil {
				return // drained for now
			}
			select {
			case claimed <- job.QueueID.String():
			case <-joinDone:
				return
			}
		}
	}
	for w := 0; w < workers; w++ {
		worker := "w" + string(rune('a'+w))
		go claim(worker)
	}
	select {
	case <-joinDone:
	case <-time.After(30 * time.Second):
		t.Fatal("concurrent claiming did not drain the queue in time")
	}
}

// TestQueueStaleClaimRecovery verifies the visibility timeout: a claimed job
// whose heartbeat lapsed is reclaimable, attempts increment, and a fresh
// heartbeat protects an active claim.
func TestQueueStaleClaimRecovery(t *testing.T) {
	svc, st := newService(t)
	svc = svc.WithQueueMode()
	ctx := context.Background()
	repo := makeProjectWithRepo(t, st, "q3")

	if _, err := svc.EnqueuePipelineRun(ctx, repo, platform.TriggerInput{
		WorkflowPath: ".github/workflows/test.yml", Ref: "main", Event: "push",
	}, queueWorkflowYAML, "deadbeef", uuid.NullUUID{}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	job, err := svc.ClaimQueuedJob(ctx, "worker-a")
	if err != nil || job == nil {
		t.Fatalf("claim: %v %v", job, err)
	}
	if job.Attempts != 1 {
		t.Fatalf("first claim attempts = %d, want 1", job.Attempts)
	}

	// Heartbeat keeps the claim fresh: an immediate re-claim finds nothing.
	ok, err := svc.HeartbeatQueuedJob(ctx, "worker-a", job.QueueID)
	if err != nil || !ok {
		t.Fatalf("heartbeat: ok=%v err=%v", ok, err)
	}
	if again, _ := svc.ClaimQueuedJob(ctx, "worker-b"); again != nil {
		t.Fatal("heartbeat-refreshed job must not be reclaimable")
	}

	// Simulate a dead worker: age the claim past the visibility timeout.
	_, err = st.Pool().Exec(ctx,
		"UPDATE pipeline_queue SET claimed_at = now() - interval '15 minutes' WHERE id = $1", job.QueueID)
	if err != nil {
		t.Fatalf("age claim: %v", err)
	}

	reclaimed, err := svc.ClaimQueuedJob(ctx, "worker-b")
	if err != nil || reclaimed == nil {
		t.Fatalf("reclaim: %v %v", reclaimed, err)
	}
	if reclaimed.QueueID != job.QueueID {
		t.Fatal("reclaim should return the same queue row")
	}
	if reclaimed.Attempts != 2 {
		t.Fatalf("reclaimed attempts = %d, want 2", reclaimed.Attempts)
	}

	// The dead worker's heartbeat is refused: the claim changed hands.
	if ok, _ := svc.HeartbeatQueuedJob(ctx, "worker-a", job.QueueID); ok {
		t.Fatal("stale worker heartbeat must be refused")
	}
}

// TestQueueReportPersistsResult drives the worker round-trip: claim, report a
// success result with a job/step tree, then an error path — and verifies the
// runs, steps, and queue rows end in the right states.
func TestQueueReportPersistsResult(t *testing.T) {
	svc, st := newService(t)
	svc = svc.WithQueueMode()
	ctx := context.Background()
	repo := makeProjectWithRepo(t, st, "q4")

	run, err := svc.EnqueuePipelineRun(ctx, repo, platform.TriggerInput{
		WorkflowPath: ".github/workflows/test.yml", Ref: "main", Event: "push",
	}, queueWorkflowYAML, "deadbeef", uuid.NullUUID{})
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	job, err := svc.ClaimQueuedJob(ctx, "worker-a")
	if err != nil || job == nil {
		t.Fatalf("claim: %v %v", job, err)
	}

	now := time.Now()
	if err := svc.ReportQueuedJob(ctx, "worker-a", job.QueueID, &pipeline.RunResult{
		Status:     pipeline.StatusSuccess,
		StartedAt:  now,
		FinishedAt: now,
		Jobs: []pipeline.JobResult{{
			Key: "build", Name: "build", Status: pipeline.StatusSuccess, Position: 1,
			StartedAt: now, FinishedAt: now,
			Steps: []pipeline.StepResult{{
				Name: "echo", Type: pipeline.StepTypeRun, Status: pipeline.StatusSuccess,
				Logs: "hello\n", StartedAt: now, FinishedAt: now,
			}},
		}},
	}, ""); err != nil {
		t.Fatalf("report: %v", err)
	}

	got, err := st.GetPipelineRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("reload run: %v", err)
	}
	if got.Status != pipeline.StatusSuccess {
		t.Fatalf("reported run status = %q, want success", got.Status)
	}
	if !got.FinishedAt.Valid {
		t.Fatal("reported run should carry a finished_at")
	}
	jobID := mustRunJobID(t, st, run.ID)
	steps, err := st.ListStepsByJob(ctx, jobID)
	if err != nil {
		t.Fatalf("list steps: %v", err)
	}
	if len(steps) != 1 || steps[0].Logs != "hello\n" {
		t.Fatalf("persisted steps = %+v", steps)
	}

	// Reporting an execution error records a startup failure instead.
	run2, err := svc.EnqueuePipelineRun(ctx, repo, platform.TriggerInput{
		WorkflowPath: ".github/workflows/test.yml", Ref: "main", Event: "push",
	}, queueWorkflowYAML, "deadbeef", uuid.NullUUID{})
	if err != nil {
		t.Fatalf("enqueue 2: %v", err)
	}
	job2, err := svc.ClaimQueuedJob(ctx, "worker-a")
	if err != nil || job2 == nil {
		t.Fatalf("claim 2: %v %v", job2, err)
	}
	if err := svc.ReportQueuedJob(ctx, "worker-a", job2.QueueID, nil, "docker: connection refused"); err != nil {
		t.Fatalf("report 2: %v", err)
	}
	got2, err := st.GetPipelineRun(ctx, run2.ID)
	if err != nil {
		t.Fatalf("reload run 2: %v", err)
	}
	if got2.Status != pipeline.StatusFailure {
		t.Fatalf("errored run status = %q, want failure", got2.Status)
	}
}

func mustRunJobID(t *testing.T, st *store.Store, runID uuid.UUID) uuid.UUID {
	t.Helper()
	jobs, err := st.ListJobsByRun(context.Background(), runID)
	if err != nil {
		t.Fatalf("list jobs: %v", err)
	}
	if len(jobs) != 1 {
		t.Fatalf("expected 1 persisted job, got %d", len(jobs))
	}
	return jobs[0].ID
}
