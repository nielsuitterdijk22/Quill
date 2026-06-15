package pipeline

import (
	"context"
	"os"
	"strings"
	"testing"
)

// TestMatchesEvent verifies `on:` gating used to filter webhook-triggered runs.
func TestMatchesEvent(t *testing.T) {
	const wf = `on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: echo hi
`
	if ok, err := MatchesEvent(wf, "push"); err != nil || !ok {
		t.Fatalf("MatchesEvent(push) = %v, %v; want true, nil", ok, err)
	}
	if ok, err := MatchesEvent(wf, "pull_request"); err != nil || ok {
		t.Fatalf("MatchesEvent(pull_request) = %v, %v; want false, nil", ok, err)
	}
}

// TestMatchesEventMultiple checks a workflow that lists several events.
func TestMatchesEventMultiple(t *testing.T) {
	const wf = `on: [push, pull_request]
jobs:
  t:
    runs-on: ubuntu-latest
    steps: [{ run: "true" }]
`
	for _, ev := range []string{"push", "pull_request"} {
		if ok, err := MatchesEvent(wf, ev); err != nil || !ok {
			t.Fatalf("MatchesEvent(%s) = %v, %v; want true", ev, ok, err)
		}
	}
	if ok, _ := MatchesEvent(wf, "release"); ok {
		t.Fatal("MatchesEvent(release) = true; want false")
	}
}

// TestRunInvalidWorkflowErrors verifies an unparseable workflow returns an error
// so the platform layer records a startup failure (no Docker needed).
func TestRunInvalidWorkflowErrors(t *testing.T) {
	_, err := NewActRunner().Run(context.Background(), JobSpec{WorkflowYAML: ":\n  not: valid: yaml: ["})
	if err == nil {
		t.Fatal("expected error for invalid workflow YAML")
	}
}

// TestRunNoMatchingEventSkips verifies that a workflow with no job for the event
// yields a skipped run rather than an error or a spurious job (no Docker needed).
func TestRunNoMatchingEventSkips(t *testing.T) {
	const wf = `on: pull_request
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: echo hi
`
	res, err := NewActRunner().Run(context.Background(), JobSpec{WorkflowYAML: wf, Event: "push"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Status != StatusSkipped || len(res.Jobs) != 0 {
		t.Fatalf("res = %+v; want skipped with no jobs", res)
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

// TestActRunnerExecutesOnDocker is the real end-to-end check: it runs a workflow
// through act on the host's container engine and asserts the step ran, logs were
// captured, and status rolled up. It is gated on QUILL_PIPELINE_DOCKER_TEST so
// `make be-test` stays fast and Docker-free; set it (and optionally
// QUILL_PIPELINE_UBUNTU_IMAGE) to exercise the full path.
func TestActRunnerExecutesOnDocker(t *testing.T) {
	if os.Getenv("QUILL_PIPELINE_DOCKER_TEST") == "" {
		t.Skip("set QUILL_PIPELINE_DOCKER_TEST=1 to run the Docker-backed pipeline test")
	}
	const wf = `name: CI
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: echo hello-from-step
`
	res, err := NewActRunner().Run(context.Background(), JobSpec{WorkflowYAML: wf, Event: "manual"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Status != StatusSuccess {
		t.Fatalf("run status = %q, want success", res.Status)
	}
	if len(res.Jobs) != 1 || res.Jobs[0].Key != "build" || res.Jobs[0].Status != StatusSuccess {
		t.Fatalf("job = %+v, want one success job 'build'", res.Jobs)
	}
	var logs string
	for _, s := range res.Jobs[0].Steps {
		logs += s.Logs
	}
	if !strings.Contains(logs, "hello-from-step") {
		t.Fatalf("step logs missing output; got %q", logs)
	}
}

// TestActRunnerRunsUsesAction verifies that `uses:` action steps actually
// execute (the old runner skipped them). It runs a real JS action through act,
// so it needs Docker and network; gated like the other Docker tests.
func TestActRunnerRunsUsesAction(t *testing.T) {
	if os.Getenv("QUILL_PIPELINE_DOCKER_TEST") == "" {
		t.Skip("set QUILL_PIPELINE_DOCKER_TEST=1 to run the Docker-backed pipeline test")
	}
	const wf = `on: push
jobs:
  greet:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/hello-world-javascript-action@main
        with:
          who-to-greet: Quill
`
	res, err := NewActRunner().Run(context.Background(), JobSpec{WorkflowYAML: wf, Event: "manual"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Status != StatusSuccess {
		t.Fatalf("run status = %q, want success", res.Status)
	}
	var sawUses bool
	for _, s := range res.Jobs[0].Steps {
		if s.Type == StepTypeUses && s.Status == StatusSuccess {
			sawUses = true
		}
	}
	if !sawUses {
		t.Fatalf("no successful uses step in %+v", res.Jobs[0].Steps)
	}
}

// TestActRunnerFailingStepFailsRun verifies a non-zero exit fails the run (Docker).
func TestActRunnerFailingStepFailsRun(t *testing.T) {
	if os.Getenv("QUILL_PIPELINE_DOCKER_TEST") == "" {
		t.Skip("set QUILL_PIPELINE_DOCKER_TEST=1 to run the Docker-backed pipeline test")
	}
	const wf = `on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: exit 3
`
	res, err := NewActRunner().Run(context.Background(), JobSpec{WorkflowYAML: wf, Event: "manual"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Status != StatusFailure {
		t.Fatalf("run status = %q, want failure", res.Status)
	}
}
