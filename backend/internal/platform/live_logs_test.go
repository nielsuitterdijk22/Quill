package platform_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nielsuitterdijk22/quill/internal/pipeline"
	"github.com/nielsuitterdijk22/quill/internal/platform"
)

// Live run output (2026-11-14): queue workers ship lines while the job runs;
// viewers on any api instance read them back. Lines must be masked before
// they are stored, only the claim holder may write, and the buffer is dropped
// once the final result is persisted.
func TestQueueLiveLogs(t *testing.T) {
	svc, st := newService(t)
	svc = svc.WithQueueMode()
	ctx := context.Background()
	repo := makeProjectWithRepo(t, st, "qlive")
	project, err := st.GetProjectByID(ctx, repo.ProjectID)
	if err != nil {
		t.Fatal(err)
	}
	admin := platform.Actor{UserID: makeUser(t, st, "qlive-admin"), IsAdmin: true}
	const secret = "s3cr3t-live-91ac"
	if _, err := svc.SetRepoSecret(ctx, admin, project.Slug, repo.Slug, "LIVE_SECRET", secret); err != nil {
		t.Fatalf("set secret: %v", err)
	}
	const wf = ".github/workflows/test.yml"
	run, err := svc.EnqueuePipelineRun(ctx, repo, platform.TriggerInput{WorkflowPath: wf, Ref: "main", Event: "push"}, queueWorkflowYAML, "deadbeef", uuid.NullUUID{})
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	var job *platform.QueuedJob
	for i := 0; i < 50; i++ {
		j, err := svc.ClaimQueuedJob(ctx, "worker-live")
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

	lines := []platform.LiveLine{
		{JobKey: "build", StepName: "echo", Line: "hello\n"},
		{JobKey: "build", StepName: "echo", Line: "token=" + secret + "\n"},
	}
	if err := svc.AppendQueuedJobLogs(ctx, "someone-else", job.QueueID, lines); !errors.Is(err, platform.ErrConflict) {
		t.Fatalf("non-holder write: want ErrConflict, got %v", err)
	}
	if err := svc.AppendQueuedJobLogs(ctx, "worker-live", job.QueueID, lines); err != nil {
		t.Fatalf("append: %v", err)
	}

	live, detail, err := svc.OpenRunLive(ctx, admin, project.Slug, repo.Slug, wf, int(run.RunNumber))
	if err != nil {
		t.Fatalf("open live: %v", err)
	}
	if detail.Run.Status != "running" {
		t.Fatalf("claimed run status = %q, want running", detail.Run.Status)
	}
	r, got, err := live.Poll(ctx, 0, 100)
	if err != nil || len(got) != 2 || r.Status != "running" {
		t.Fatalf("poll: %v %d %q", err, len(got), r.Status)
	}
	if got[0].Line != "hello\n" || strings.Contains(got[1].Line, secret) {
		t.Fatalf("lines not stored masked/in order: %q", []string{got[0].Line, got[1].Line})
	}
	if _, rest, _ := live.Poll(ctx, got[0].ID, 100); len(rest) != 1 || rest[0].ID != got[1].ID {
		t.Fatalf("resume after id: %+v", rest)
	}

	now := time.Now()
	if err := svc.ReportQueuedJob(ctx, "worker-live", job.QueueID, &pipeline.RunResult{
		Status: pipeline.StatusSuccess, StartedAt: now, FinishedAt: now,
		Jobs: []pipeline.JobResult{{Key: "build", Name: "build", Status: pipeline.StatusSuccess, Position: 1, StartedAt: now, FinishedAt: now,
			Steps: []pipeline.StepResult{{Name: "echo", Type: pipeline.StepTypeRun, Status: pipeline.StatusSuccess, Logs: "hello\n", StartedAt: now, FinishedAt: now}}}},
	}, ""); err != nil {
		t.Fatalf("report: %v", err)
	}
	r, got, err = live.Poll(ctx, 0, 100)
	if err != nil || len(got) != 0 || r.Status != "success" {
		t.Fatalf("after report: %v lines=%d status=%q (buffer must be dropped)", err, len(got), r.Status)
	}
	d, err := live.Detail(ctx)
	if err != nil || len(d.Jobs) != 1 || len(d.Jobs[0].Steps) != 1 {
		t.Fatalf("detail: %v %+v", err, d.Jobs)
	}
}
