// Package pipeline contains Quill's CI execution layer: it interprets
// GitHub Actions-style workflows (parsed with nektos/act's model package) and
// executes them through a pluggable Runner.
//
// The Runner interface is the seam that lets the executor be swapped without
// touching the platform service. The API can call an HTTP dispatcher, while the
// dispatcher uses actRunner to interpret and execute a workflow through a
// configured Docker engine. A Forge-compatible runner can drop in behind the
// same dispatch contract later.
package pipeline

import (
	"context"
	"io"
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

// LogSink is a callback invoked for each raw output line produced during a
// pipeline run. jobKey identifies the job; stepName is the current step's
// display name; line is the log line (always newline-terminated). It must not
// block for long — implementations should write to a buffered channel.
type LogSink func(jobKey, stepName, line string)

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
	// CloneURL is an optional git URL the runner can clone when the caller did not
	// provide Workdir. It must never contain credentials.
	CloneURL string
	// CloneAuthHeader is an optional HTTP header used only for git checkout. It
	// may contain credentials and must never be written to logs or workflow env.
	CloneAuthHeader string
	// RepoFullName is "owner/name", used to populate the synthetic event payload
	// so `github.repository`-style expressions resolve. Optional.
	RepoFullName string
	// Token is an optional credential exposed to the workflow as GITHUB_TOKEN.
	// Empty is fine — act skips actions/checkout and runs against Workdir.
	Token string
	// LogSink is called for each raw output line during execution. It is omitted
	// from JSON serialisation so it survives only in the dispatching process.
	LogSink LogSink `json:"-"`
}

// LogStreamer is an optional interface that Runner implementations may satisfy
// to allow the platform layer to proxy live log events to SSE clients.
type LogStreamer interface {
	// StreamLogs connects to the dispatcher job identified by jobID and returns
	// the raw SSE byte stream. The caller owns the returned ReadCloser.
	StreamLogs(ctx context.Context, jobID string) (io.ReadCloser, error)
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

// CloneAuth is the sanitized URL plus secret header needed for runner-side git
// checkout.
type CloneAuth struct {
	URL        string
	AuthHeader string
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
