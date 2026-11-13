package platform_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nielsuitterdijk22/quill/internal/pipeline"
	"github.com/nielsuitterdijk22/quill/internal/platform"
)

// Queue mode (Phase A, live since 2026-11-13) persisted worker results WITHOUT
// the secret masking the inline dispatch path applies: a step that echoed a
// secret — and act's own debug expression traces ("evaluated to
// '%!t(string=<value>)'") — landed in run logs in clear text (found by the
// pipeline showcase). Every reported log line and error must be masked with
// the run's own secret values.
func TestQueueReportMasksSecrets(t *testing.T) {
	svc, st := newService(t)
	svc = svc.WithQueueMode()
	ctx := context.Background()
	repo := makeProjectWithRepo(t, st, "qmask")
	project, err := st.GetProjectByID(ctx, repo.ProjectID)
	if err != nil {
		t.Fatal(err)
	}
	admin := platform.Actor{UserID: makeUser(t, st, "qmask-admin"), IsAdmin: true}
	const secret = "s3cr3t-value-qmask-7d1f"
	if _, err := svc.SetRepoSecret(ctx, admin, project.Slug, repo.Slug, "DEMO_SECRET", secret); err != nil {
		t.Fatalf("set secret: %v", err)
	}

	run, err := svc.EnqueuePipelineRun(ctx, repo, platform.TriggerInput{
		WorkflowPath: ".github/workflows/test.yml", Ref: "main", Event: "push",
	}, queueWorkflowYAML, "deadbeef", uuid.NullUUID{})
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	var job *platform.QueuedJob
	for i := 0; i < 50; i++ {
		j, err := svc.ClaimQueuedJob(ctx, "worker-mask")
		if err != nil || j == nil {
			t.Fatalf("claim: %v %v", j, err)
		}
		if j.RunID == run.ID {
			job = j
			break
		}
	}
	if job == nil {
		t.Fatal("job not claimed")
	}
	if job.Spec.Secrets["DEMO_SECRET"] != secret {
		t.Fatal("worker spec must still carry the real secret (it injects it)")
	}

	now := time.Now()
	leaky := "echo: " + secret + "\nexpression 'format('{0}', secrets.DEMO_SECRET)' evaluated to '%!t(string=" + secret + ")'\n"
	if err := svc.ReportQueuedJob(ctx, "worker-mask", job.QueueID, &pipeline.RunResult{
		Status: pipeline.StatusSuccess, StartedAt: now, FinishedAt: now,
		Jobs: []pipeline.JobResult{{
			Key: "build", Name: "build", Status: pipeline.StatusSuccess, Position: 1,
			StartedAt: now, FinishedAt: now,
			Steps: []pipeline.StepResult{{
				Name: "echo", Type: pipeline.StepTypeRun, Status: pipeline.StatusSuccess,
				Logs: leaky, StartedAt: now, FinishedAt: now,
			}},
		}},
	}, ""); err != nil {
		t.Fatalf("report: %v", err)
	}
	steps, err := st.ListStepsByJob(ctx, mustRunJobID(t, st, run.ID))
	if err != nil || len(steps) != 1 {
		t.Fatalf("steps: %v %d", err, len(steps))
	}
	if strings.Contains(steps[0].Logs, secret) {
		t.Fatalf("secret persisted in clear text: %q", steps[0].Logs)
	}
	if !strings.Contains(steps[0].Logs, "***") {
		t.Fatalf("no masked marker in persisted logs: %q", steps[0].Logs)
	}
}

// Runs reported before the fix kept clear-text secrets in their stored logs,
// and finished queue rows kept the secrets themselves at rest. The remask
// pass masks each run's logs with that run's own secret values, then scrubs
// the secrets out of the finished queue row; a second pass is a no-op.
func TestRemaskStoredRunLogsAndScrubQueue(t *testing.T) {
	svc, st := newService(t)
	svc = svc.WithQueueMode()
	ctx := context.Background()
	repo := makeProjectWithRepo(t, st, "qremask")
	project, _ := st.GetProjectByID(ctx, repo.ProjectID)
	admin := platform.Actor{UserID: makeUser(t, st, "qremask-admin"), IsAdmin: true}
	const secret = "legacy-leak-9a2c-value"
	if _, err := svc.SetRepoSecret(ctx, admin, project.Slug, repo.Slug, "TOKEN", secret); err != nil {
		t.Fatal(err)
	}
	run, err := svc.EnqueuePipelineRun(ctx, repo, platform.TriggerInput{WorkflowPath: ".github/workflows/test.yml", Ref: "main", Event: "push"}, queueWorkflowYAML, "deadbeef", uuid.NullUUID{})
	if err != nil {
		t.Fatal(err)
	}
	var job *platform.QueuedJob
	for i := 0; i < 50 && job == nil; i++ {
		j, err := svc.ClaimQueuedJob(ctx, "worker-remask")
		if err != nil || j == nil {
			t.Fatalf("claim: %v", err)
		}
		if j.RunID == run.ID {
			job = j
		}
	}
	now := time.Now()
	if err := svc.ReportQueuedJob(ctx, "worker-remask", job.QueueID, &pipeline.RunResult{
		Status: pipeline.StatusSuccess, StartedAt: now, FinishedAt: now,
		Jobs: []pipeline.JobResult{{Key: "b", Name: "b", Status: pipeline.StatusSuccess, Position: 1, StartedAt: now, FinishedAt: now,
			Steps: []pipeline.StepResult{{Name: "s", Type: pipeline.StepTypeRun, Status: pipeline.StatusSuccess, Logs: "ok\n", StartedAt: now, FinishedAt: now}}}},
	}, ""); err != nil {
		t.Fatal(err)
	}
	// Simulate a pre-fix row: clear-text secret in the stored logs, and the
	// secrets still present in the finished queue spec.
	jobID := mustRunJobID(t, st, run.ID)
	if _, err := st.Pool().Exec(ctx, "UPDATE pipeline_steps SET logs = $2 WHERE job_id = $1", jobID, "token="+secret+"\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Pool().Exec(ctx,
		`UPDATE pipeline_queue SET spec = jsonb_set(spec, '{Secrets}', jsonb_build_object('TOKEN', $2::text)) WHERE id = $1`,
		job.QueueID, secret); err != nil {
		t.Fatal(err)
	}

	runs, steps, err := svc.RemaskStoredRunLogs(ctx)
	if err != nil {
		t.Fatalf("remask: %v", err)
	}
	if runs < 1 || steps < 1 {
		t.Fatalf("remask touched runs=%d steps=%d", runs, steps)
	}
	got, _ := st.ListStepsByJob(ctx, jobID)
	if strings.Contains(got[0].Logs, secret) || !strings.Contains(got[0].Logs, "***") {
		t.Fatalf("logs after remask: %q", got[0].Logs)
	}
	var stillSecret bool
	if err := st.Pool().QueryRow(ctx, "SELECT spec::text LIKE '%'||$2||'%' FROM pipeline_queue WHERE id = $1", job.QueueID, secret).Scan(&stillSecret); err != nil {
		t.Fatal(err)
	}
	if stillSecret {
		t.Fatal("finished queue row still holds the secret")
	}
}

// After a successful report the finished queue row no longer holds secrets.
func TestQueueReportScrubsSpecSecrets(t *testing.T) {
	svc, st := newService(t)
	svc = svc.WithQueueMode()
	ctx := context.Background()
	repo := makeProjectWithRepo(t, st, "qscrub")
	project, _ := st.GetProjectByID(ctx, repo.ProjectID)
	admin := platform.Actor{UserID: makeUser(t, st, "qscrub-admin"), IsAdmin: true}
	const secret = "scrub-me-4b8e-value"
	if _, err := svc.SetRepoSecret(ctx, admin, project.Slug, repo.Slug, "TOKEN", secret); err != nil {
		t.Fatal(err)
	}
	run, err := svc.EnqueuePipelineRun(ctx, repo, platform.TriggerInput{WorkflowPath: ".github/workflows/test.yml", Ref: "main", Event: "push"}, queueWorkflowYAML, "deadbeef", uuid.NullUUID{})
	if err != nil {
		t.Fatal(err)
	}
	var job *platform.QueuedJob
	for i := 0; i < 50 && job == nil; i++ {
		j, err := svc.ClaimQueuedJob(ctx, "worker-scrub")
		if err != nil || j == nil {
			t.Fatalf("claim: %v", err)
		}
		if j.RunID == run.ID {
			job = j
		}
	}
	if err := svc.ReportQueuedJob(ctx, "worker-scrub", job.QueueID, nil, "boom"); err != nil {
		t.Fatal(err)
	}
	var still bool
	if err := st.Pool().QueryRow(ctx, "SELECT spec::text LIKE '%'||$2||'%' FROM pipeline_queue WHERE id = $1", job.QueueID, secret).Scan(&still); err != nil {
		t.Fatal(err)
	}
	if still {
		t.Fatal("finished queue row still holds the secret")
	}
}
