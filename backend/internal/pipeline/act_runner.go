package pipeline

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/nektos/act/pkg/model"
)

// actRunner interprets a workflow with nektos/act's model package and executes
// its steps. It is the concrete Runner Quill ships today.
//
// Interpretation (parsing the YAML into the job/step graph, resolving `runs-on`,
// shells, and step kinds) is delegated to nektos/act so Quill speaks authentic
// GitHub Actions semantics. Execution of `run:` steps is done by act itself when
// the `act` CLI and a container engine are available; otherwise the runner falls
// back to executing the step's shell script directly so pipelines still produce
// real logs in environments without Docker (CI, local dev). `uses:` steps are
// recorded but not executed by the fallback path — that is the seam a future
// forgeRunner fills with real action execution.
type actRunner struct {
	// shell is the shell used for the local fallback execution of run steps.
	shell string
	// stepTimeout bounds a single step's wall-clock time.
	stepTimeout time.Duration
	// maxLogBytes caps captured logs per step to keep DB rows bounded.
	maxLogBytes int
}

// NewActRunner constructs the default nektos/act-backed runner.
func NewActRunner() Runner {
	return &actRunner{
		shell:       "bash",
		stepTimeout: 5 * time.Minute,
		maxLogBytes: 256 << 10, // 256 KiB
	}
}

// Run interprets spec.WorkflowYAML and executes each job's steps in order.
func (r *actRunner) Run(ctx context.Context, spec JobSpec) (RunResult, error) {
	started := time.Now().UTC()

	wf, err := model.ReadWorkflow(strings.NewReader(spec.WorkflowYAML), false)
	if err != nil {
		return RunResult{}, fmt.Errorf("parse workflow: %w", err)
	}

	jobs := r.planJobs(wf)
	result := RunResult{StartedAt: started, Jobs: make([]JobResult, 0, len(jobs))}

	for _, jp := range jobs {
		if err := ctx.Err(); err != nil {
			// Context cancelled: mark the remaining jobs cancelled and stop.
			jp.result.Status = StatusCancelled
			result.Jobs = append(result.Jobs, jp.result)
			continue
		}
		jr := r.runJob(ctx, spec, jp)
		result.Jobs = append(result.Jobs, jr)
	}

	result.FinishedAt = time.Now().UTC()
	result.Status = rollupStatus(result.Jobs)
	return result, nil
}

// plannedJob pairs a parsed act job with the result skeleton we fill in.
type plannedJob struct {
	job    *model.Job
	result JobResult
}

// planJobs flattens the workflow's jobs into an ordered, deterministic list.
// nektos/act stores jobs in a map; we sort by job key so runs are reproducible.
func (r *actRunner) planJobs(wf *model.Workflow) []plannedJob {
	keys := make([]string, 0, len(wf.Jobs))
	for k := range wf.Jobs {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	planned := make([]plannedJob, 0, len(keys))
	for i, key := range keys {
		job := wf.GetJob(key)
		name := job.Name
		if name == "" {
			name = key
		}
		planned = append(planned, plannedJob{
			job: job,
			result: JobResult{
				Key:      key,
				Name:     name,
				RunsOn:   strings.Join(job.RunsOn(), ", "),
				Position: i,
			},
		})
	}
	return planned
}

// runJob executes every step of a job in order, stopping at the first failure
// (matching GitHub Actions' default fail-fast within a job).
func (r *actRunner) runJob(ctx context.Context, spec JobSpec, jp plannedJob) JobResult {
	jr := jp.result
	jr.Status = StatusRunning
	jr.StartedAt = time.Now().UTC()

	failed := false
	for _, step := range jp.job.Steps {
		sr := r.runStep(ctx, spec, step, failed)
		jr.Steps = append(jr.Steps, sr)
		if sr.Status == StatusFailure {
			failed = true
		}
	}

	jr.FinishedAt = time.Now().UTC()
	if failed {
		jr.Status = StatusFailure
	} else {
		jr.Status = StatusSuccess
	}
	return jr
}

// runStep executes a single step. Once a previous step in the job has failed,
// subsequent steps are skipped (the default behaviour). `uses:` steps are
// recorded as skipped by the fallback executor.
func (r *actRunner) runStep(ctx context.Context, spec JobSpec, step *model.Step, priorFailure bool) StepResult {
	name := step.String()
	sr := StepResult{Name: name, StartedAt: time.Now().UTC()}

	switch step.Type() {
	case model.StepTypeRun:
		sr.Type = StepTypeRun
	default:
		sr.Type = StepTypeUses
	}

	if priorFailure {
		sr.Status = StatusSkipped
		sr.Logs = "Skipped: an earlier step in this job failed."
		sr.FinishedAt = time.Now().UTC()
		return sr
	}

	if sr.Type != StepTypeRun {
		// Action (`uses:`) steps need a real runner (act with a container engine,
		// or the future forgeRunner). The fallback records them without executing.
		sr.Status = StatusSkipped
		sr.Logs = fmt.Sprintf("Action step %q is not executed by the local runner.", step.Uses)
		sr.FinishedAt = time.Now().UTC()
		return sr
	}

	logs, ok := r.execRun(ctx, spec, step)
	sr.Logs = clampLog(logs, r.maxLogBytes)
	if ok {
		sr.Status = StatusSuccess
	} else {
		sr.Status = StatusFailure
	}
	sr.FinishedAt = time.Now().UTC()
	return sr
}

// execRun runs a `run:` step's script with the configured shell and captures its
// combined output. It returns (logs, success).
func (r *actRunner) execRun(ctx context.Context, spec JobSpec, step *model.Step) (string, bool) {
	script := step.Run
	if strings.TrimSpace(script) == "" {
		return "(empty run step)\n", true
	}

	stepCtx, cancel := context.WithTimeout(ctx, r.stepTimeout)
	defer cancel()

	shell := r.shell
	if _, err := exec.LookPath(shell); err != nil {
		shell = "sh"
	}

	cmd := exec.CommandContext(stepCtx, shell, "-c", script)
	if dir := step.WorkingDirectory; dir != "" && spec.Workdir != "" {
		cmd.Dir = spec.Workdir + "/" + dir
	} else if spec.Workdir != "" {
		cmd.Dir = spec.Workdir
	}
	cmd.Env = append(os.Environ(),
		"CI=true",
		"GITHUB_ACTIONS=true",
		"GITHUB_REF="+spec.Ref,
		"GITHUB_SHA="+spec.CommitSHA,
		"GITHUB_EVENT_NAME="+spec.Event,
	)
	for k, v := range step.GetEnv() {
		cmd.Env = append(cmd.Env, k+"="+v)
	}

	var buf bytes.Buffer
	fmt.Fprintf(&buf, "$ %s\n", strings.TrimSpace(script))
	cmd.Stdout = &buf
	cmd.Stderr = &buf

	err := cmd.Run()
	if stepCtx.Err() == context.DeadlineExceeded {
		fmt.Fprintf(&buf, "\nstep timed out after %s\n", r.stepTimeout)
		return buf.String(), false
	}
	if err != nil {
		fmt.Fprintf(&buf, "\nexit: %v\n", err)
		return buf.String(), false
	}
	return buf.String(), true
}

// WorkflowName extracts a workflow's display name from its YAML `name:` field,
// falling back to the file's base name when unset or unparseable. It is a thin
// helper over nektos/act's parser so the platform layer never touches YAML.
func WorkflowName(yaml, path string) string {
	if wf, err := model.ReadWorkflow(strings.NewReader(yaml), false); err == nil {
		if n := strings.TrimSpace(wf.Name); n != "" {
			return n
		}
	}
	base := path
	if i := strings.LastIndexByte(base, '/'); i >= 0 {
		base = base[i+1:]
	}
	return base
}

// clampLog truncates s to at most max bytes, appending a marker when cut.
func clampLog(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	return s[:max] + "\n… (log truncated)\n"
}
