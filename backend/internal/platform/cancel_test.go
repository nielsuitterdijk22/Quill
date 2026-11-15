package platform_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/nielsuitterdijk22/quill/internal/pipeline"
	"github.com/nielsuitterdijk22/quill/internal/platform"
	"github.com/nielsuitterdijk22/quill/internal/store/db"
)

// Cancelling a queue-mode run that is still waiting for a worker ('pending')
// works and withdraws its queue row; run numbers count per workflow, so the
// cancel touches only the named workflow's run (2026-11-15: pending runs
// could not be cancelled, and #N of every workflow was cancelled together).
func TestCancelPendingRunScopedToWorkflow(t *testing.T) {
	svc, st := newService(t)
	svc = svc.WithQueueMode()
	ctx := context.Background()
	repo := makeProjectWithRepo(t, st, "cancel")
	project, err := st.GetProjectByID(ctx, repo.ProjectID)
	if err != nil {
		t.Fatal(err)
	}
	admin := platform.Actor{UserID: makeUser(t, st, "cancel-admin"), IsAdmin: true}
	enqueue := func(path string) db.PipelineRun {
		r, err := svc.EnqueuePipelineRun(ctx, repo, platform.TriggerInput{WorkflowPath: path, Ref: "main", Event: "push"}, queueWorkflowYAML, "c1", uuid.NullUUID{})
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	a, b := enqueue(".github/workflows/a.yml"), enqueue(".github/workflows/b.yml")
	if a.RunNumber != b.RunNumber || a.Status != pipeline.StatusPending {
		t.Fatalf("fixture: a=%d/%s b=%d", a.RunNumber, a.Status, b.RunNumber)
	}
	if err := svc.CancelRun(ctx, admin, project.Slug, repo.Slug, ".github/workflows/a.yml", int(a.RunNumber)); err != nil {
		t.Fatalf("cancel pending: %v", err)
	}
	status := func(path string, n int64) string {
		_, d, err := svc.GetRun(ctx, admin, project.Slug, repo.Slug, path, int(n))
		if err != nil {
			t.Fatal(err)
		}
		return d.Run.Status
	}
	if s := status(".github/workflows/a.yml", a.RunNumber); s != pipeline.StatusCancelled {
		t.Fatalf("a = %s, want cancelled", s)
	}
	if s := status(".github/workflows/b.yml", b.RunNumber); s != pipeline.StatusPending {
		t.Fatalf("b = %s: cancel leaked into another workflow", s)
	}
	for i := 0; i < 10; i++ {
		j, err := svc.ClaimQueuedJob(ctx, "cancel-worker")
		if err != nil || j == nil {
			break
		}
		if j.RunID == a.ID {
			t.Fatal("cancelled run was still claimable")
		}
	}
	if err := svc.CancelRun(ctx, admin, project.Slug, repo.Slug, ".github/workflows/a.yml", int(a.RunNumber)); !errors.Is(err, platform.ErrConflict) {
		t.Fatalf("second cancel: %v", err)
	}
}
