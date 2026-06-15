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
func (s *Service) ListPipelines(ctx context.Context, actor Actor, orgSlug, repoSlug string) (db.Repository, []PipelineSummary, error) {
	repo, owner, name, err := s.resolveRepo(ctx, actor, orgSlug, repoSlug, true)
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
func (s *Service) ListRuns(ctx context.Context, actor Actor, orgSlug, repoSlug string) (db.Repository, []db.PipelineRun, error) {
	repo, _, _, err := s.resolveRepo(ctx, actor, orgSlug, repoSlug, false)
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
func (s *Service) GetRun(ctx context.Context, actor Actor, orgSlug, repoSlug, workflowPath string, runNumber int) (db.Repository, RunDetail, error) {
	repo, _, _, err := s.resolveRepo(ctx, actor, orgSlug, repoSlug, false)
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

// ---- triggering ------------------------------------------------------------

// TriggerRun runs a workflow for an authorized org member. It reads the workflow
// YAML from git, executes it through the runner, and records the run tree.
func (s *Service) TriggerRun(ctx context.Context, actor Actor, orgSlug, repoSlug string, in TriggerInput) (db.Repository, db.PipelineRun, error) {
	repo, owner, name, err := s.resolveRepo(ctx, actor, orgSlug, repoSlug, true)
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
// push/pull_request event. It runs every workflow file found (GitHub semantics
// gate by the workflow's `on:` triggers; that filtering is a TODO — see below).
// It is not actor-scoped: the webhook is authenticated by a shared secret at the
// server layer, so runs are recorded with no triggering user.
func (s *Service) TriggerWebhook(ctx context.Context, repo db.Repository, owner, name, event, ref string) ([]db.PipelineRun, error) {
	files, err := s.forgejo.ListWorkflows(ctx, owner, name, ref)
	if err != nil {
		return nil, translateForgejoRead(err)
	}
	var runs []db.PipelineRun
	for _, f := range files {
		// TODO: respect each workflow's `on:` triggers (push branches/paths,
		// pull_request) before running. nektos/act's model exposes On(); wiring
		// the event/branch filter here is the next refinement.
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

// runWorkflow is the shared trigger path: it loads the workflow YAML, ensures a
// pipeline record, executes the workflow through the runner, and persists the
// run/job/step tree. The run is recorded even when execution fails so the failure
// is visible in the UI.
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

	result, runErr := s.runner.Run(ctx, pipeline.JobSpec{
		WorkflowYAML: yaml,
		WorkflowPath: path,
		Event:        event,
		Ref:          ref,
		CommitSHA:    commitSHA,
	})
	if runErr != nil {
		// The workflow could not be interpreted/started: mark the run failed and
		// surface the reason as a single synthetic step so the UI shows it.
		s.recordStartupFailure(ctx, run.ID, runErr)
		return s.finishRun(ctx, run.ID, pipeline.StatusFailure)
	}

	if err := s.persistResult(ctx, run.ID, result); err != nil {
		s.logger.Warn("could not persist run result", "run", run.ID, "error", err)
	}
	return s.finishRun(ctx, run.ID, result.Status)
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
// the repos in each org because Forgejo's owner is an org-level name; the set of
// orgs/repos is small for the PoC.
func (s *Service) resolveRepoForWebhook(ctx context.Context, owner, name string) (db.Repository, string, string, error) {
	orgs, err := s.store.ListOrganizations(ctx, db.ListOrganizationsParams{Limit: 500, Offset: 0})
	if err != nil {
		return db.Repository{}, "", "", fmt.Errorf("list orgs: %w", err)
	}
	for _, org := range orgs {
		repos, err := s.store.ListRepositoriesByOrg(ctx, db.ListRepositoriesByOrgParams{OrgID: org.ID, Limit: 500, Offset: 0})
		if err != nil {
			return db.Repository{}, "", "", fmt.Errorf("list repos: %w", err)
		}
		for _, repo := range repos {
			fo, fn, ok := forgejoTarget(repo, org)
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
