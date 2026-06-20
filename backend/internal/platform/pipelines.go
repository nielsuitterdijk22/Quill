package platform

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nielsuitterdijk22/quill/internal/pipeline"
	"github.com/nielsuitterdijk22/quill/internal/store/db"
)

// This file implements pipelines (CI). Workflow definitions live in the git
// repository (.github/workflows, read through Forgejo's contents API); Quill
// reads them, interprets each through the pluggable Runner (nektos/act today,
// Forge later), and records the run/job/step tree plus logs in Postgres. Runs
// are triggered manually through the API or automatically by a Forgejo webhook
// (see TriggerWebhook).

// maxRunsListed caps how many runs a listing returns.
const maxRunsListed = 100

// ---- inputs / outputs ------------------------------------------------------

// PipelineSummary pairs a workflow file with the persisted pipeline record (when
// one exists yet) so the frontend can render the overview before the first run.
type PipelineSummary struct {
	WorkflowPath string
	Name         string
	Pipeline     *db.Pipeline
	LastRun      *db.PipelineRun
}

// RunDetail is a run with its fully expanded job/step tree.
type RunDetail struct {
	Run      db.PipelineRun
	Pipeline db.Pipeline
	Jobs     []JobDetail
}

// JobDetail is a job and its steps within a run.
type JobDetail struct {
	Job   db.PipelineJob
	Steps []db.PipelineStep
}

// TriggerInput describes a request to run a workflow.
type TriggerInput struct {
	// WorkflowPath is the repo-relative path of the workflow to run.
	WorkflowPath string
	// Ref is the branch (or other ref) to build; empty means the default branch.
	Ref string
	// Event is the trigger kind ("manual", "push", "pull_request").
	Event string
}

// ---- listing ---------------------------------------------------------------

// ListPipelines returns the repository's workflow files (from .github/workflows)
// joined with any persisted pipeline + its most recent run, so the overview can
// show CI status even for workflows that have never run.
func (s *Service) ListPipelines(ctx context.Context, actor Actor, projectSlug, repoSlug string) (db.Repository, []PipelineSummary, error) {
	repo, owner, name, err := s.resolveRepo(ctx, actor, projectSlug, repoSlug, true)
	if err != nil {
		return db.Repository{}, nil, err
	}
	files, err := s.forgejo.ListWorkflows(ctx, owner, name, "")
	if err != nil {
		return db.Repository{}, nil, translateForgejoRead(err)
	}
	persisted, err := s.store.ListPipelinesByRepo(ctx, repo.ID)
	if err != nil {
		return db.Repository{}, nil, fmt.Errorf("list pipelines: %w", err)
	}
	byPath := make(map[string]db.Pipeline, len(persisted))
	for _, p := range persisted {
		byPath[p.WorkflowPath] = p
	}

	out := make([]PipelineSummary, 0, len(files))
	for _, f := range files {
		sum := PipelineSummary{WorkflowPath: f.Path, Name: f.Name}
		if p, ok := byPath[f.Path]; ok {
			pp := p
			sum.Pipeline = &pp
			if p.Name != "" {
				sum.Name = p.Name
			}
			runs, err := s.store.ListRunsByPipeline(ctx, db.ListRunsByPipelineParams{PipelineID: p.ID, Limit: 1})
			if err != nil {
				return db.Repository{}, nil, fmt.Errorf("list runs: %w", err)
			}
			if len(runs) > 0 {
				r := runs[0]
				sum.LastRun = &r
			}
		}
		out = append(out, sum)
	}
	return repo, out, nil
}

// ListRuns returns the most recent runs across every pipeline in a repository.
func (s *Service) ListRuns(ctx context.Context, actor Actor, projectSlug, repoSlug string) (db.Repository, []db.PipelineRun, error) {
	repo, _, _, err := s.resolveRepo(ctx, actor, projectSlug, repoSlug, false)
	if err != nil {
		return db.Repository{}, nil, err
	}
	runs, err := s.store.ListRunsByRepo(ctx, db.ListRunsByRepoParams{RepoID: repo.ID, Limit: maxRunsListed})
	if err != nil {
		return db.Repository{}, nil, fmt.Errorf("list runs: %w", err)
	}
	return repo, runs, nil
}

// GetRun returns a single run (by its per-pipeline run number) with its full
// job/step tree.
func (s *Service) GetRun(ctx context.Context, actor Actor, projectSlug, repoSlug, workflowPath string, runNumber int) (db.Repository, RunDetail, error) {
	repo, _, _, err := s.resolveRepo(ctx, actor, projectSlug, repoSlug, false)
	if err != nil {
		return db.Repository{}, RunDetail{}, err
	}
	pl, err := s.store.GetPipelineByPath(ctx, db.GetPipelineByPathParams{RepoID: repo.ID, WorkflowPath: workflowPath})
	if errors.Is(err, pgx.ErrNoRows) {
		return db.Repository{}, RunDetail{}, ErrNotFound
	}
	if err != nil {
		return db.Repository{}, RunDetail{}, fmt.Errorf("lookup pipeline: %w", err)
	}
	run, err := s.store.GetPipelineRunByNumber(ctx, db.GetPipelineRunByNumberParams{PipelineID: pl.ID, RunNumber: int64(runNumber)})
	if errors.Is(err, pgx.ErrNoRows) {
		return db.Repository{}, RunDetail{}, ErrNotFound
	}
	if err != nil {
		return db.Repository{}, RunDetail{}, fmt.Errorf("lookup run: %w", err)
	}

	jobs, err := s.store.ListJobsByRun(ctx, run.ID)
	if err != nil {
		return db.Repository{}, RunDetail{}, fmt.Errorf("list jobs: %w", err)
	}
	detail := RunDetail{Run: run, Pipeline: pl, Jobs: make([]JobDetail, 0, len(jobs))}
	for _, j := range jobs {
		steps, err := s.store.ListStepsByJob(ctx, j.ID)
		if err != nil {
			return db.Repository{}, RunDetail{}, fmt.Errorf("list steps: %w", err)
		}
		detail.Jobs = append(detail.Jobs, JobDetail{Job: j, Steps: steps})
	}
	return repo, detail, nil
}

// CancelRun transitions a running or queued pipeline run to cancelled. It
// returns ErrNotFound when the run doesn't exist and ErrConflict when it has
// already finished.
func (s *Service) CancelRun(ctx context.Context, actor Actor, projectSlug, repoSlug string, runNumber int) error {
	repo, _, _, err := s.resolveRepo(ctx, actor, projectSlug, repoSlug, false)
	if err != nil {
		return err
	}
	cancelled, err := s.store.CancelRun(ctx, repo.ID, int64(runNumber))
	if err != nil {
		return fmt.Errorf("cancel run: %w", err)
	}
	if !cancelled {
		return ErrConflict
	}
	return nil
}

// ---- triggering ------------------------------------------------------------

// TriggerRun runs a workflow for an authorized project member. It reads the workflow
// YAML from git, executes it through the runner, and records the run tree.
func (s *Service) TriggerRun(ctx context.Context, actor Actor, projectSlug, repoSlug string, in TriggerInput) (db.Repository, db.PipelineRun, error) {
	repo, owner, name, err := s.resolveRepo(ctx, actor, projectSlug, repoSlug, true)
	if err != nil {
		return db.Repository{}, db.PipelineRun{}, err
	}
	triggeredBy := uuid.NullUUID{UUID: actor.UserID, Valid: true}
	run, err := s.runWorkflow(ctx, repo, owner, name, in, triggeredBy)
	if err != nil {
		return db.Repository{}, db.PipelineRun{}, err
	}
	return repo, run, nil
}

// TriggerWebhook runs the workflows in a repository in response to a Forgejo
// push/pull_request event. Each workflow is gated by its `on:` triggers — only
// those that listen for this event run, matching GitHub semantics (per-event
// branch/path filters are still evaluated by the runner at execution time). It
// is not actor-scoped: the webhook is authenticated by a shared secret at the
// server layer, so runs are recorded with no triggering user.
func (s *Service) TriggerWebhook(ctx context.Context, repo db.Repository, owner, name, event, ref string) ([]db.PipelineRun, error) {
	files, err := s.forgejo.ListWorkflows(ctx, owner, name, ref)
	if err != nil {
		return nil, translateForgejoRead(err)
	}
	var runs []db.PipelineRun
	for _, f := range files {
		yaml, err := s.forgejo.GetWorkflowContent(ctx, owner, name, f.Path, ref)
		if err != nil {
			s.logger.Warn("webhook workflow read failed", "repo", owner+"/"+name, "workflow", f.Path, "error", err)
			continue
		}
		match, err := pipeline.MatchesEvent(yaml, event)
		if err != nil {
			s.logger.Warn("webhook workflow parse failed", "repo", owner+"/"+name, "workflow", f.Path, "error", err)
			continue
		}
		if !match {
			continue
		}
		run, err := s.runWorkflow(ctx, repo, owner, name, TriggerInput{
			WorkflowPath: f.Path,
			Ref:          ref,
			Event:        event,
		}, uuid.NullUUID{})
		if err != nil {
			s.logger.Warn("webhook workflow run failed", "repo", owner+"/"+name, "workflow", f.Path, "error", err)
			continue
		}
		runs = append(runs, run)
	}
	return runs, nil
}

// runWorkflow is the shared trigger path: it loads the workflow YAML, creates
// a run record, and kicks off execution in a goroutine so callers get an
// immediate response. The goroutine persists the full job/step tree when done.
func (s *Service) runWorkflow(ctx context.Context, repo db.Repository, owner, name string, in TriggerInput, triggeredBy uuid.NullUUID) (db.PipelineRun, error) {
	path := strings.TrimSpace(in.WorkflowPath)
	if path == "" {
		return db.PipelineRun{}, fmt.Errorf("%w: a workflow path is required", ErrInvalidInput)
	}
	ref := strings.TrimSpace(in.Ref)
	if ref == "" {
		ref = repo.DefaultBranch
	}
	event := strings.TrimSpace(in.Event)
	if event == "" {
		event = "manual"
	}

	yaml, err := s.forgejo.GetWorkflowContent(ctx, owner, name, path, ref)
	if err != nil {
		return db.PipelineRun{}, translateForgejoRead(err)
	}

	workflowName := pipeline.WorkflowName(yaml, path)
	pl, err := s.store.UpsertPipeline(ctx, db.UpsertPipelineParams{
		RepoID:       repo.ID,
		WorkflowPath: path,
		Name:         workflowName,
	})
	if err != nil {
		return db.PipelineRun{}, fmt.Errorf("upsert pipeline: %w", err)
	}

	commitSHA := s.resolveCommitSHA(ctx, owner, name, ref)

	next, err := s.store.NextRunNumber(ctx, pl.ID)
	if err != nil {
		return db.PipelineRun{}, fmt.Errorf("next run number: %w", err)
	}
	run, err := s.store.CreatePipelineRun(ctx, db.CreatePipelineRunParams{
		PipelineID:  pl.ID,
		RunNumber:   int64(next),
		Status:      pipeline.StatusRunning,
		Event:       event,
		Ref:         ref,
		CommitSha:   commitSHA,
		TriggeredBy: triggeredBy,
		StartedAt:   nowTS(),
	})
	if err != nil {
		return db.PipelineRun{}, fmt.Errorf("create run: %w", err)
	}

	cloneAuth := s.cloneAuthForRun(owner, name)

	bc := newLogBroadcaster()
	s.activeRuns.Store(run.ID.String(), bc)

	go s.executeRun(run.ID, bc, pipeline.JobSpec{
		WorkflowYAML:    yaml,
		WorkflowPath:    path,
		Event:           event,
		Ref:             ref,
		CommitSHA:       commitSHA,
		CloneURL:        cloneAuth.URL,
		CloneAuthHeader: cloneAuth.AuthHeader,
		RepoFullName:    owner + "/" + name,
	})

	return run, nil
}

// executeRun carries out the actual workflow execution in a background goroutine.
// It wires the log broadcaster to the spec, runs the workflow, persists results,
// and then finalises the run status in the database.
func (s *Service) executeRun(runID uuid.UUID, bc *logBroadcaster, spec pipeline.JobSpec) {
	// Use background context: the triggering HTTP request will be gone by the
	// time the pipeline finishes.
	bgCtx := context.Background()

	defer func() {
		// Keep the broadcaster alive for a few minutes so late SSE subscribers
		// can replay buffered output. Panic safety: finish ensures done is set.
		if r := recover(); r != nil {
			s.logger.Error("pipeline goroutine panicked", "run", runID, "panic", r)
			bc.finish(pipeline.StatusFailure)
		}
		time.AfterFunc(5*time.Minute, func() { s.activeRuns.Delete(runID.String()) })
	}()

	spec.LogSink = func(jobKey, stepName, line string) {
		bc.publish(LogLine{JobKey: jobKey, StepName: stepName, Line: line})
	}

	result, runErr := s.runner.Run(bgCtx, spec)
	if runErr != nil {
		s.recordStartupFailure(bgCtx, runID, runErr)
		if _, err := s.finishRun(bgCtx, runID, pipeline.StatusFailure); err != nil {
			s.logger.Warn("could not record startup failure", "run", runID, "error", err)
		}
		bc.finish(pipeline.StatusFailure)
		return
	}

	if err := s.persistResult(bgCtx, runID, result); err != nil {
		s.logger.Warn("could not persist run result", "run", runID, "error", err)
	}
	if _, err := s.finishRun(bgCtx, runID, result.Status); err != nil {
		s.logger.Warn("could not finish run", "run", runID, "error", err)
	}
	bc.finish(result.Status)
}

// cloneAuthForRun returns the clone URL and auth header for the dispatcher. On
// any failure (Forgejo disabled, URL error) it returns empty so the runner falls
// back to an empty workspace and records startup/step failures normally.
func (s *Service) cloneAuthForRun(owner, name string) pipeline.CloneAuth {
	if !s.forgejoEnabled() {
		return pipeline.CloneAuth{}
	}
	cloneAuth, err := s.forgejo.CloneAuth(owner, name)
	if err != nil {
		s.logger.Warn("pipeline clone URL unavailable", "repo", owner+"/"+name, "error", err)
		return pipeline.CloneAuth{}
	}
	return pipeline.CloneAuth{URL: cloneAuth.URL, AuthHeader: cloneAuth.AuthHeader}
}

// persistResult writes the job/step tree produced by the runner.
func (s *Service) persistResult(ctx context.Context, runID uuid.UUID, result pipeline.RunResult) error {
	for _, jr := range result.Jobs {
		job, err := s.store.CreatePipelineJob(ctx, db.CreatePipelineJobParams{
			RunID:      runID,
			JobKey:     jr.Key,
			Name:       jr.Name,
			RunsOn:     jr.RunsOn,
			Status:     jr.Status,
			Position:   int32(jr.Position),
			StartedAt:  toTS(jr.StartedAt),
			FinishedAt: toTS(jr.FinishedAt),
		})
		if err != nil {
			return fmt.Errorf("create job: %w", err)
		}
		for i, sr := range jr.Steps {
			if _, err := s.store.CreatePipelineStep(ctx, db.CreatePipelineStepParams{
				JobID:      job.ID,
				Position:   int32(i),
				Name:       sr.Name,
				StepType:   sr.Type,
				Status:     sr.Status,
				Logs:       sr.Logs,
				StartedAt:  toTS(sr.StartedAt),
				FinishedAt: toTS(sr.FinishedAt),
			}); err != nil {
				return fmt.Errorf("create step: %w", err)
			}
		}
	}
	return nil
}

// recordStartupFailure records a single failed job/step describing why a run
// could not start (e.g. invalid workflow YAML).
func (s *Service) recordStartupFailure(ctx context.Context, runID uuid.UUID, cause error) {
	job, err := s.store.CreatePipelineJob(ctx, db.CreatePipelineJobParams{
		RunID:      runID,
		JobKey:     "workflow",
		Name:       "Workflow",
		Status:     pipeline.StatusFailure,
		Position:   0,
		StartedAt:  nowTS(),
		FinishedAt: nowTS(),
	})
	if err != nil {
		s.logger.Warn("could not record startup failure job", "run", runID, "error", err)
		return
	}
	if _, err := s.store.CreatePipelineStep(ctx, db.CreatePipelineStepParams{
		JobID:      job.ID,
		Position:   0,
		Name:       "Load workflow",
		StepType:   pipeline.StepTypeRun,
		Status:     pipeline.StatusFailure,
		Logs:       cause.Error(),
		StartedAt:  nowTS(),
		FinishedAt: nowTS(),
	}); err != nil {
		s.logger.Warn("could not record startup failure step", "run", runID, "error", err)
	}
}

// finishRun marks a run terminal with the given status and reloads it.
func (s *Service) finishRun(ctx context.Context, runID uuid.UUID, status string) (db.PipelineRun, error) {
	run, err := s.store.UpdatePipelineRunStatus(ctx, db.UpdatePipelineRunStatusParams{
		ID:         runID,
		Status:     status,
		FinishedAt: nowTS(),
	})
	if err != nil {
		return db.PipelineRun{}, fmt.Errorf("finish run: %w", err)
	}
	return run, nil
}

// resolveCommitSHA best-effort resolves the tip commit of ref. An unknown ref or
// transient error yields an empty SHA rather than failing the run.
func (s *Service) resolveCommitSHA(ctx context.Context, owner, name, ref string) string {
	commits, err := s.forgejo.ListCommits(ctx, owner, name, ref, "", 1)
	if err != nil || len(commits) == 0 {
		return ""
	}
	return commits[0].SHA
}

// nowTS returns a non-null pgtype.Timestamptz set to the current UTC time.
func nowTS() pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true}
}

// toTS converts a time.Time to a pgtype.Timestamptz, treating the zero time as
// SQL NULL.
func toTS(t time.Time) pgtype.Timestamptz {
	if t.IsZero() {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: t.UTC(), Valid: true}
}

// resolveRepoForWebhook resolves the repository a Forgejo webhook targets by its
// Forgejo owner/name, returning the Quill repo plus its Forgejo target. It scans
// the repos in each project because Forgejo's owner is a project-level name; the set of
// projects/repos is small for the PoC.
func (s *Service) resolveRepoForWebhook(ctx context.Context, owner, name string) (db.Repository, string, string, error) {
	projects, err := s.store.ListProjects(ctx, db.ListProjectsParams{Limit: 500, Offset: 0})
	if err != nil {
		return db.Repository{}, "", "", fmt.Errorf("list projects: %w", err)
	}
	for _, project := range projects {
		repos, err := s.store.ListRepositoriesByProject(ctx, db.ListRepositoriesByProjectParams{ProjectID: project.ID, Limit: 500, Offset: 0})
		if err != nil {
			return db.Repository{}, "", "", fmt.Errorf("list repos: %w", err)
		}
		for _, repo := range repos {
			fo, fn, ok := forgejoTarget(repo, project)
			if ok && strings.EqualFold(fo, owner) && strings.EqualFold(fn, name) {
				return repo, fo, fn, nil
			}
		}
	}
	return db.Repository{}, "", "", ErrNotFound
}

// HandleWebhookEvent is the entry point for the webhook receiver: it maps a
// Forgejo push/pull_request payload to a repository and triggers its workflows.
func (s *Service) HandleWebhookEvent(ctx context.Context, owner, name, event, ref string) ([]db.PipelineRun, error) {
	if !s.forgejoEnabled() {
		return nil, ErrUnavailable
	}
	repo, fo, fn, err := s.resolveRepoForWebhook(ctx, owner, name)
	if err != nil {
		return nil, err
	}
	return s.TriggerWebhook(ctx, repo, fo, fn, event, ref)
}

// GetRunStream returns a LogStream for the given pipeline run. If the run is
// still active its live broadcaster is returned; if it has already completed a
// static stream synthesised from the stored step logs is returned instead.
func (s *Service) GetRunStream(ctx context.Context, actor Actor, projectSlug, repoSlug, workflowPath string, runNumber int) (LogStream, db.PipelineRun, error) {
	repo, _, _, err := s.resolveRepo(ctx, actor, projectSlug, repoSlug, false)
	if err != nil {
		return nil, db.PipelineRun{}, err
	}

	pl, err := s.store.GetPipelineByPath(ctx, db.GetPipelineByPathParams{RepoID: repo.ID, WorkflowPath: workflowPath})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, db.PipelineRun{}, ErrNotFound
	}
	if err != nil {
		return nil, db.PipelineRun{}, fmt.Errorf("lookup pipeline: %w", err)
	}

	run, err := s.store.GetPipelineRunByNumber(ctx, db.GetPipelineRunByNumberParams{PipelineID: pl.ID, RunNumber: int64(runNumber)})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, db.PipelineRun{}, ErrNotFound
	}
	if err != nil {
		return nil, db.PipelineRun{}, fmt.Errorf("lookup run: %w", err)
	}

	// If the run is still active, return its live broadcaster.
	if val, ok := s.activeRuns.Load(run.ID.String()); ok {
		return val.(*logBroadcaster), run, nil
	}

	// Run is complete: synthesise a static stream from stored step logs.
	jobs, err := s.store.ListJobsByRun(ctx, run.ID)
	if err != nil {
		return nil, db.PipelineRun{}, fmt.Errorf("list jobs: %w", err)
	}
	var lines []LogLine
	for _, j := range jobs {
		steps, err := s.store.ListStepsByJob(ctx, j.ID)
		if err != nil {
			return nil, db.PipelineRun{}, fmt.Errorf("list steps: %w", err)
		}
		for _, st := range steps {
			if st.Logs == "" {
				continue
			}
			for _, line := range splitLines(st.Logs) {
				lines = append(lines, LogLine{JobKey: j.JobKey, StepName: st.Name, Line: line})
			}
		}
	}
	return &staticLogStream{lines: lines, status: run.Status}, run, nil
}

// splitLines splits a log string into individual newline-terminated lines.
func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, s[start:i+1])
			start = i + 1
		}
	}
	if start < len(s) {
		out = append(out, s[start:]+"\n")
	}
	return out
}
