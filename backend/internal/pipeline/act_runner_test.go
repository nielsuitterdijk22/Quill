package pipeline

import (
	"context"
	"strings"
	"testing"
)

// TestActRunnerRunsShellSteps verifies the runner interprets a workflow with
// nektos/act and executes its `run:` steps, capturing logs and rolling status up.
func TestActRunnerRunsShellSteps(t *testing.T) {
	const wf = `name: CI
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: echo hello-from-step
`
	r := NewActRunner()
	res, err := r.Run(context.Background(), JobSpec{WorkflowYAML: wf, Event: "manual"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Status != StatusSuccess {
		t.Fatalf("run status = %q, want success", res.Status)
	}
	if len(res.Jobs) != 1 {
		t.Fatalf("jobs = %d, want 1", len(res.Jobs))
	}
	job := res.Jobs[0]
	if job.Key != "build" || job.Status != StatusSuccess {
		t.Fatalf("job = %+v, want key=build status=success", job)
	}
	if len(job.Steps) != 1 || job.Steps[0].Status != StatusSuccess {
		t.Fatalf("steps = %+v, want one success step", job.Steps)
	}
	if !strings.Contains(job.Steps[0].Logs, "hello-from-step") {
		t.Fatalf("logs missing step output: %q", job.Steps[0].Logs)
	}
}

// TestActRunnerFailingStepFailsJob verifies a non-zero exit fails the step, the
// job, and the run, and that later steps are skipped.
func TestActRunnerFailingStepFailsJob(t *testing.T) {
	const wf = `on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: exit 3
      - run: echo should-not-run
`
	res, err := NewActRunner().Run(context.Background(), JobSpec{WorkflowYAML: wf})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Status != StatusFailure {
		t.Fatalf("run status = %q, want failure", res.Status)
	}
	steps := res.Jobs[0].Steps
	if len(steps) != 2 {
		t.Fatalf("steps = %d, want 2", len(steps))
	}
	if steps[0].Status != StatusFailure {
		t.Fatalf("step[0] status = %q, want failure", steps[0].Status)
	}
	if steps[1].Status != StatusSkipped {
		t.Fatalf("step[1] status = %q, want skipped", steps[1].Status)
	}
}

// TestActRunnerInvalidWorkflowErrors verifies an unparseable workflow returns an
// error (so the platform layer records a startup failure).
func TestActRunnerInvalidWorkflowErrors(t *testing.T) {
	_, err := NewActRunner().Run(context.Background(), JobSpec{WorkflowYAML: ":\n  not: valid: yaml: ["})
	if err == nil {
		t.Fatal("expected error for invalid workflow YAML")
	}
}

// TestWorkflowName checks name extraction and the file-base fallback.
func TestWorkflowName(t *testing.T) {
	if got := WorkflowName("name: My CI\non: push\njobs: {}\n", ".github/workflows/ci.yml"); got != "My CI" {
		t.Fatalf("WorkflowName = %q, want My CI", got)
	}
	if got := WorkflowName("on: push\njobs: {}\n", ".github/workflows/ci.yml"); got != "ci.yml" {
		t.Fatalf("WorkflowName fallback = %q, want ci.yml", got)
	}
}
