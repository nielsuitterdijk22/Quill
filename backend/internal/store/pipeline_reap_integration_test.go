package store_test

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nielsuitterdijk22/quill/internal/store/db"
)

// Stuck runs are failed, healthy ones are not, and a duplicate event for a
// commit that is already waiting resolves to the existing run.
func TestReapStuckRunsAndFindInflight(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, os.Getenv("QUILL_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	suffix := uuid.NewString()[:8]

	tenant, err := st.GetTenantBySlug(ctx, "default")
	if err != nil {
		t.Fatal(err)
	}
	project, err := st.CreateProject(ctx, db.CreateProjectParams{TenantID: tenant.ID, Slug: "reap-" + suffix, Name: "Reap " + suffix})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanupProject(t, st, project.ID) })
	repo, err := st.CreateRepository(ctx, db.CreateRepositoryParams{ProjectID: project.ID, Slug: "r-" + suffix, Name: "R", Visibility: "private", DefaultBranch: "main"})
	if err != nil {
		t.Fatal(err)
	}
	pl, err := st.UpsertPipeline(ctx, db.UpsertPipelineParams{RepoID: repo.ID, WorkflowPath: ".github/workflows/ci.yml", Name: "ci"})
	if err != nil {
		t.Fatal(err)
	}

	n := int64(0)
	newRun := func(status, event, sha string) db.PipelineRun {
		n++
		r, err := st.CreatePipelineRun(ctx, db.CreatePipelineRunParams{PipelineID: pl.ID, RunNumber: n, Status: status, Event: event, Ref: "refs/heads/x", CommitSha: sha})
		if err != nil {
			t.Fatal(err)
		}
		if err := st.EnqueuePipelineJob(ctx, r.ID, uuid.NullUUID{}, []byte(`{"Secrets":{"A":"b"}}`)); err != nil {
			t.Fatal(err)
		}
		return r
	}
	status := func(id uuid.UUID) string {
		var s string
		if err := pool.QueryRow(ctx, `SELECT status FROM pipeline_runs WHERE id=$1`, id).Scan(&s); err != nil {
			t.Fatal(err)
		}
		return s
	}

	abandoned := newRun("running", "push", "aaa") // claimed 3x, heartbeat lapsed
	healthy := newRun("running", "push", "bbb")   // claimed, heartbeat fresh
	old := newRun("pending", "push", "ccc")       // nobody picked it up for hours
	fresh := newRun("pending", "push", "ddd")     // just queued
	finished := newRun("success", "push", "eee")  // already done, queue row stale

	exec := func(q string, args ...any) {
		if _, err := pool.Exec(ctx, q, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`UPDATE pipeline_queue SET status='claimed', attempts=3, claimed_by='w', claimed_at=now()-interval '30 minutes' WHERE run_id=$1`, abandoned.ID)
	exec(`UPDATE pipeline_queue SET status='claimed', attempts=3, claimed_by='w', claimed_at=now() WHERE run_id=$1`, healthy.ID)
	exec(`UPDATE pipeline_queue SET created_at=now()-interval '3 hours' WHERE run_id=$1`, old.ID)
	exec(`UPDATE pipeline_queue SET status='claimed', attempts=3, claimed_by='w', claimed_at=now()-interval '30 minutes' WHERE run_id=$1`, finished.ID)

	gone, unclaimed, err := st.ReapStuckPipelineRuns(ctx, 10*time.Minute, 3, 90*time.Minute)
	if err != nil {
		t.Fatalf("reap: %v", err)
	}
	if len(gone) != 1 || gone[0] != abandoned.ID {
		t.Errorf("abandoned = %v, want [%v]", gone, abandoned.ID)
	}
	if len(unclaimed) != 1 || unclaimed[0] != old.ID {
		t.Errorf("unclaimed = %v, want [%v]", unclaimed, old.ID)
	}
	for id, want := range map[uuid.UUID]string{abandoned.ID: "failure", old.ID: "failure", healthy.ID: "running", fresh.ID: "pending", finished.ID: "success"} {
		if got := status(id); got != want {
			t.Errorf("run %v status = %q, want %q", id, got, want)
		}
	}
	var secrets *string
	if err := pool.QueryRow(ctx, `SELECT spec->>'Secrets' FROM pipeline_queue WHERE run_id=$1`, abandoned.ID).Scan(&secrets); err != nil || secrets != nil {
		t.Errorf("reaped queue row must drop its secrets: %v %v", secrets, err)
	}

	// Duplicate detection: same pipeline+event+ref+sha already waiting.
	dup, err := st.FindInflightRun(ctx, pl.ID, "push", "refs/heads/x", "ddd")
	if err != nil || dup.ID != fresh.ID {
		t.Errorf("inflight = %v, %v; want run %v", dup.ID, err, fresh.ID)
	}
	if _, err := st.FindInflightRun(ctx, pl.ID, "pull_request", "refs/heads/x", "ddd"); !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("a different event must not match: %v", err)
	}
	if _, err := st.FindInflightRun(ctx, pl.ID, "push", "refs/heads/x", "eee"); !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("a finished run must not count as in flight: %v", err)
	}
}
