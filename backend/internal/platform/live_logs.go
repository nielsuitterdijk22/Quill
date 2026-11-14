package platform

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/nielsuitterdijk22/quill/internal/store"
	"github.com/nielsuitterdijk22/quill/internal/store/db"
)

// Live pipeline output (2026-11-14). Workers run on separate machines and the
// api runs as several serverless instances, so live log lines are buffered in
// Postgres (pipeline_live_logs) rather than in one process's memory: any api
// instance can serve any viewer, and a page reload simply replays the buffer.
// Lines are masked with the run's secrets BEFORE they are stored. The buffer
// is dropped once the final result (with its durable step logs) is persisted.

// maxLiveLinesPerReport bounds one worker log report.
const maxLiveLinesPerReport = 2000

// LiveLine is one reported output line.
type LiveLine struct {
	JobKey   string `json:"jobKey"`
	StepName string `json:"stepName"`
	Line     string `json:"line"`
}

// AppendQueuedJobLogs stores a worker's live output for the run behind
// queueID. Refused (ErrConflict) unless workerID still holds the claim, so a
// reclaimed worker cannot keep writing into someone else's run.
func (s *Service) AppendQueuedJobLogs(ctx context.Context, workerID string, queueID uuid.UUID, lines []LiveLine) error {
	if len(lines) > maxLiveLinesPerReport {
		return fmt.Errorf("%w: at most %d lines per report", ErrInvalidInput, maxLiveLinesPerReport)
	}
	row, err := s.store.GetPipelineQueueJob(ctx, queueID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("load queue job: %w", err)
	}
	if row.ClaimedBy != workerID || row.Status != "claimed" {
		return ErrConflict
	}
	var spec struct {
		Secrets map[string]string
	}
	if err := json.Unmarshal(row.Spec, &spec); err != nil {
		// Cannot mask → store nothing (fail closed, as ReportQueuedJob does).
		return nil
	}
	masker := newSecretMasker(spec.Secrets)
	out := make([]store.LiveLogLine, 0, len(lines))
	for _, l := range lines {
		out = append(out, store.LiveLogLine{JobKey: l.JobKey, StepName: l.StepName, Line: masker.mask(l.Line)})
	}
	return s.store.AppendLiveLogs(ctx, row.RunID, out)
}

// RunLive follows one run for a live viewer. Authorization happens once, in
// OpenRunLive; polling reads by run id.
type RunLive struct {
	svc      *Service
	pipeline db.Pipeline
	runID    uuid.UUID
}

// OpenRunLive authorizes actor for the run and returns its current detail
// plus a follower for polling.
func (s *Service) OpenRunLive(ctx context.Context, actor Actor, projectSlug, repoSlug, workflowPath string, runNumber int) (*RunLive, RunDetail, error) {
	_, detail, err := s.GetRun(ctx, actor, projectSlug, repoSlug, workflowPath, runNumber)
	if err != nil {
		return nil, RunDetail{}, err
	}
	return &RunLive{svc: s, pipeline: detail.Pipeline, runID: detail.Run.ID}, detail, nil
}

// Poll returns the run row and up to limit buffered lines after id `after`.
func (l *RunLive) Poll(ctx context.Context, after int64, limit int) (db.PipelineRun, []store.LiveLogLine, error) {
	run, err := l.svc.store.GetPipelineRun(ctx, l.runID)
	if err != nil {
		return db.PipelineRun{}, nil, fmt.Errorf("load run: %w", err)
	}
	lines, err := l.svc.store.ListLiveLogs(ctx, l.runID, after, limit)
	if err != nil {
		return db.PipelineRun{}, nil, fmt.Errorf("load live logs: %w", err)
	}
	return run, lines, nil
}

// Detail reloads the run with its persisted job/step tree.
func (l *RunLive) Detail(ctx context.Context) (RunDetail, error) {
	run, err := l.svc.store.GetPipelineRun(ctx, l.runID)
	if err != nil {
		return RunDetail{}, fmt.Errorf("load run: %w", err)
	}
	jobs, err := l.svc.store.ListJobsByRun(ctx, run.ID)
	if err != nil {
		return RunDetail{}, fmt.Errorf("list jobs: %w", err)
	}
	detail := RunDetail{Run: run, Pipeline: l.pipeline, Jobs: make([]JobDetail, 0, len(jobs))}
	for _, j := range jobs {
		steps, err := l.svc.store.ListStepsByJob(ctx, j.ID)
		if err != nil {
			return RunDetail{}, fmt.Errorf("list steps: %w", err)
		}
		detail.Jobs = append(detail.Jobs, JobDetail{Job: j, Steps: steps})
	}
	return detail, nil
}

// liveLogBuffer batches inline-mode output lines into pipeline_live_logs so
// the inline runner feeds the same live surface as queue workers.
type liveLogBuffer struct {
	svc   *Service
	runID uuid.UUID
	mu    sync.Mutex
	buf   []store.LiveLogLine
	stop  chan struct{}
	done  chan struct{}
}

func (s *Service) newLiveLogBuffer(runID uuid.UUID) *liveLogBuffer {
	b := &liveLogBuffer{svc: s, runID: runID, stop: make(chan struct{}), done: make(chan struct{})}
	go b.loop()
	return b
}

func (b *liveLogBuffer) add(jobKey, stepName, line string) {
	b.mu.Lock()
	b.buf = append(b.buf, store.LiveLogLine{JobKey: jobKey, StepName: stepName, Line: line})
	b.mu.Unlock()
}

func (b *liveLogBuffer) flush() {
	b.mu.Lock()
	lines := b.buf
	b.buf = nil
	b.mu.Unlock()
	if len(lines) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := b.svc.store.AppendLiveLogs(ctx, b.runID, lines); err != nil {
		b.svc.logger.Warn("live log flush failed", "run", b.runID, "error", err)
	}
}

func (b *liveLogBuffer) loop() {
	defer close(b.done)
	t := time.NewTicker(500 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-b.stop:
			b.flush()
			return
		case <-t.C:
			b.flush()
		}
	}
}

// close flushes the remainder and stops the loop.
func (b *liveLogBuffer) close() {
	close(b.stop)
	<-b.done
}
