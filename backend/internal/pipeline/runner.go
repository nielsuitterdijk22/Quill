// Package pipeline contains Quill's CI execution layer: it interprets
// GitHub Actions-style workflows (parsed with nektos/act's model package) and
// executes them through a pluggable Runner.
//
// The Runner interface is the seam that lets the executor be swapped without
// touching the platform service. Today the only implementation is actRunner,
// which uses nektos/act to interpret and fully execute a workflow through a
// configured Docker engine; a forgeRunner that dispatches the same JobSpec to
// Forge's ephemeral, confidential runners can drop in behind the same interface
// later.
package pipeline

import (
	"context"
	"time"
)

// Status values for runs, jobs, and steps. They mirror the strings stored in
// Postgres so the runner, platform service, and frontend agree on a vocabulary.
const (
	StatusPending   = "pending"
	StatusRunning   = "running"
	StatusSuccess   = "success"
	StatusFailure   = "failure"
	StatusCancelled = "cancelled"
	StatusSkipped   = "skipped"
)

// Step type discriminators stored on each step record.
const (
	StepTypeRun  = "run"
	StepTypeUses = "uses"
)

// JobSpec is everything a Runner needs to execute one workflow: the raw workflow
// YAML and the context it runs in. The Runner is responsible for interpreting the
// YAML — Quill does not pre-expand jobs/steps, so a future forgeRunner is free to
// hand the YAML to a remote executor verbatim.
type JobSpec struct {
	// WorkflowYAML is the verbatim contents of the workflow file.
	WorkflowYAML string
	// WorkflowPath is the repo-relative path of the workflow (for diagnostics).
	WorkflowPath string
	// Event is the triggering event ("manual", "push", "pull_request").
	Event string
	// Ref and CommitSHA describe what is being built.
	Ref       string
	CommitSHA string
	// Workdir is an optional checked-out working directory the runner executes
	// against. When empty the runner provisions a transient temp directory.
	Workdir string
	// RepoFullName is "owner/name", used to populate the synthetic event payload
	// so `github.repository`-style expressions resolve. Optional.
	RepoFullName string
	// Token is an optional credential exposed to the workflow as GITHUB_TOKEN.
	// Empty is fine — act skips actions/checkout and runs against Workdir.
	Token string
}

// StepResult is the outcome of a single step.
type StepResult struct {
	Name       string
	Type       string // StepTypeRun | StepTypeUses
	Status     string
	Logs       string
	StartedAt  time.Time
	FinishedAt time.Time
}

// JobResult is the outcome of a single job and the steps it ran.
type JobResult struct {
	Key        string
	Name       string
	RunsOn     string
	Status     string
	Position   int
	StartedAt  time.Time
	FinishedAt time.Time
	Steps      []StepResult
}

// RunResult is the outcome of executing a whole workflow.
type RunResult struct {
	Status     string
	StartedAt  time.Time
	FinishedAt time.Time
	Jobs       []JobResult
}

// Runner executes a workflow described by spec and returns its structured
// result. Implementations must not return a partial RunResult together with a
// non-nil error: either the workflow ran (and per-job/step status carries the
// pass/fail detail) or it could not start (error).
type Runner interface {
	Run(ctx context.Context, spec JobSpec) (RunResult, error)
}

// rollupStatus collapses per-job statuses into a single run status: failure if
// any job failed, otherwise success. An empty job set is treated as success.
func rollupStatus(jobs []JobResult) string {
	status := StatusSuccess
	for _, j := range jobs {
		if j.Status == StatusFailure {
			return StatusFailure
		}
		if j.Status == StatusCancelled {
			status = StatusCancelled
		}
	}
	return status
}
