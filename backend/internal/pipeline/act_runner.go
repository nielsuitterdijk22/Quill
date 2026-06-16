package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/nektos/act/pkg/model"
	"github.com/nektos/act/pkg/runner"
)

// actRunner is the concrete Runner Quill ships: it interprets a workflow with
// nektos/act and executes it for real through the configured Docker engine. act
// is the GitHub Actions runtime, so this gives authentic semantics — `on:`
// filtering, `uses:` actions (incl. actions/checkout), composite/Docker actions,
// matrices and expressions — without a separate Forge runner yet.
//
// In local compose, the dispatcher container reaches Docker through the mounted
// daemon socket. The Runner interface remains the seam where a Forge-compatible
// runner can later dispatch the same JobSpec to ephemeral runners instead.
type actRunner struct {
	// images maps a runs-on label to the container image act runs the job in.
	images map[string]string
	// arch is the container platform (e.g. linux/amd64) act targets.
	arch string
	// timeout bounds a whole workflow run.
	timeout time.Duration
	// maxLogBytes caps captured logs per step to keep DB rows bounded.
	maxLogBytes int
}

// NewActRunner constructs the default nektos/act-backed runner. Image and
// architecture come from the environment so deployments can pin them without a
// code change:
//
//	QUILL_PIPELINE_UBUNTU_IMAGE   image for ubuntu-* runners (default catthehacker/ubuntu:act-latest)
//	QUILL_PIPELINE_CONTAINER_ARCH container platform (default linux/<goarch>)
//	QUILL_PIPELINE_TIMEOUT        whole-run timeout (Go duration, default 30m)
func NewActRunner() Runner {
	return &actRunner{
		images:      defaultImages(),
		arch:        containerArch(),
		timeout:     pipelineTimeout(),
		maxLogBytes: 256 << 10, // 256 KiB
	}
}

// Run interprets spec.WorkflowYAML, plans it for the triggering event, and
// executes the plan with act. It returns a structured RunResult; a non-nil error
// means the workflow could not start (bad YAML, no image, Docker unreachable),
// in which case the platform layer records a startup failure.
func (r *actRunner) Run(ctx context.Context, spec JobSpec) (RunResult, error) {
	started := time.Now().UTC()

	planner, err := model.NewSingleWorkflowPlanner(workflowFileName(spec.WorkflowPath), strings.NewReader(spec.WorkflowYAML))
	if err != nil {
		return RunResult{}, fmt.Errorf("parse workflow: %w", err)
	}
	plan, err := r.plan(planner, spec.Event)
	if err != nil {
		return RunResult{}, fmt.Errorf("plan workflow: %w", err)
	}
	if plan == nil || len(plan.Stages) == 0 {
		// No job matches the triggering event: nothing to run.
		return RunResult{Status: StatusSkipped, StartedAt: started, FinishedAt: time.Now().UTC()}, nil
	}

	// act needs a workspace. Dispatchers can receive an authenticated clone URL
	// from Quill and perform checkout next to the Docker engine; without either we
	// still give act a temp dir so workflows without repo files can run.
	workdir := spec.Workdir
	if workdir == "" {
		tmp, err := os.MkdirTemp("", "quill-ci-ws-*")
		if err != nil {
			return RunResult{}, fmt.Errorf("create workspace: %w", err)
		}
		defer os.RemoveAll(tmp)
		workdir = tmp
		if strings.TrimSpace(spec.CloneURL) != "" {
			if err := Checkout(ctx, spec.CloneURL, spec.Ref, spec.CommitSHA, workdir, spec.CloneAuthHeader); err != nil {
				return RunResult{}, err
			}
		}
	}

	eventPath, cleanup, err := writeEventFile(spec)
	if err != nil {
		return RunResult{}, fmt.Errorf("write event: %w", err)
	}
	defer cleanup()

	cfg := &runner.Config{
		Workdir:               workdir,
		BindWorkdir:           false,
		EventName:             actEventName(spec.Event),
		EventPath:             eventPath,
		DefaultBranch:         "main",
		Platforms:             r.images,
		ContainerArchitecture: r.arch,
		AutoRemove:            true,
		LogOutput:             true,
		Token:                 spec.Token,
		Env:                   map[string]string{},
		Secrets:               map[string]string{},
		Vars:                  map[string]string{},
		GitHubInstance:        "github.com",
	}
	rn, err := runner.New(cfg)
	if err != nil {
		return RunResult{}, fmt.Errorf("init runner: %w", err)
	}

	cap := newLogCapture()
	exec := rn.NewPlanExecutor(plan)

	runCtx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	runCtx = runner.WithJobLoggerFactory(runCtx, cap)

	execErr := exec(runCtx)

	result := r.assemble(plan, cap, started)
	if len(result.Jobs) == 0 {
		// act produced no captured jobs — surface why it couldn't start.
		if execErr != nil {
			return RunResult{}, execErr
		}
		result.Status = StatusSkipped
	}
	return result, nil
}

// plan selects which jobs to run. A manual trigger ("Run workflow") runs every
// job regardless of the workflow's `on:`; an event trigger (push/pull_request)
// runs only the jobs whose `on:` includes that event.
func (r *actRunner) plan(planner model.WorkflowPlanner, event string) (*model.Plan, error) {
	if event == "" || event == "manual" {
		return planner.PlanAll()
	}
	return planner.PlanEvent(event)
}

// assemble turns the plan (job order/metadata) and the capture (per-step status
// and logs) into a RunResult. Job identity/order comes from the plan so the tree
// is stable even when a job is skipped and emits no logs.
func (r *actRunner) assemble(plan *model.Plan, cap *logCapture, started time.Time) RunResult {
	res := RunResult{StartedAt: started, Jobs: make([]JobResult, 0)}
	pos := 0
	for _, stage := range plan.Stages {
		for _, run := range stage.Runs {
			job := run.Job()
			jr := JobResult{
				Key:      run.JobID,
				Name:     firstNonEmpty(job.Name, run.JobID),
				RunsOn:   strings.Join(job.RunsOn(), ", "),
				Position: pos,
			}
			pos++

			typeByName := make(map[string]string, len(job.Steps))
			for _, st := range job.Steps {
				typeByName[st.String()] = stepKind(st)
			}

			jc := cap.job(run.JobID)
			if jc == nil {
				jr.Status = StatusSkipped
				res.Jobs = append(res.Jobs, jr)
				continue
			}
			jr.StartedAt = jc.started
			jr.FinishedAt = jc.finished
			for _, sid := range jc.order {
				sc := jc.steps[sid]
				jr.Steps = append(jr.Steps, StepResult{
					Name:       firstNonEmpty(sc.name, sid),
					Type:       firstNonEmpty(typeByName[sc.name], StepTypeRun),
					Status:     mapStatus(sc.outcome),
					Logs:       clampLog(sc.logs.String(), r.maxLogBytes),
					StartedAt:  sc.started,
					FinishedAt: sc.finished,
				})
			}
			jr.Status = mapJobStatus(jc.result, jr.Steps)
			res.Jobs = append(res.Jobs, jr)
		}
	}
	res.FinishedAt = time.Now().UTC()
	res.Status = rollupStatus(res.Jobs)
	return res
}

// MatchesEvent reports whether any job in the workflow runs for event. The
// platform layer uses it to gate webhook-triggered runs so a push doesn't run
// workflows that only listen for other events. Branch/path filters within an
// event are still evaluated by act at run time, not here.
func MatchesEvent(workflowYAML, event string) (bool, error) {
	planner, err := model.NewSingleWorkflowPlanner("workflow.yml", strings.NewReader(workflowYAML))
	if err != nil {
		return false, err
	}
	plan, err := planner.PlanEvent(event)
	if err != nil {
		return false, err
	}
	return plan != nil && len(plan.Stages) > 0, nil
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
	return baseName(path)
}

// ---- helpers ---------------------------------------------------------------

// stepKind classifies a parsed step as a run or uses step.
func stepKind(s *model.Step) string {
	if s.Type() == model.StepTypeRun {
		return StepTypeRun
	}
	return StepTypeUses
}

// mapStatus maps act's step outcome ("success"/"failure"/"skipped") to Quill's
// vocabulary, defaulting an unlabelled-but-seen step to success.
func mapStatus(outcome string) string {
	switch outcome {
	case "failure":
		return StatusFailure
	case "skipped":
		return StatusSkipped
	case "success", "":
		return StatusSuccess
	default:
		return StatusSuccess
	}
}

// mapJobStatus prefers act's job result; absent that it rolls up the steps.
func mapJobStatus(jobResult string, steps []StepResult) string {
	switch jobResult {
	case "success":
		return StatusSuccess
	case "failure":
		return StatusFailure
	case "skipped":
		return StatusSkipped
	}
	for _, s := range steps {
		if s.Status == StatusFailure {
			return StatusFailure
		}
	}
	return StatusSuccess
}

// actEventName maps Quill's event to the GitHub event name act runs under. A
// manual trigger presents as workflow_dispatch inside the workflow context.
func actEventName(event string) string {
	if event == "" || event == "manual" {
		return "workflow_dispatch"
	}
	return event
}

// writeEventFile writes a minimal event payload act mounts as event.json so
// expressions like github.ref / github.sha resolve. Returns a cleanup func.
func writeEventFile(spec JobSpec) (string, func(), error) {
	payload := map[string]any{
		"ref":         spec.Ref,
		"after":       spec.CommitSHA,
		"head_commit": map[string]any{"id": spec.CommitSHA},
	}
	if owner, name, ok := splitFullName(spec.RepoFullName); ok {
		payload["repository"] = map[string]any{
			"full_name":      spec.RepoFullName,
			"name":           name,
			"owner":          map[string]any{"login": owner, "name": owner},
			"default_branch": "main",
		}
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return "", func() {}, err
	}
	f, err := os.CreateTemp("", "quill-ci-event-*.json")
	if err != nil {
		return "", func() {}, err
	}
	if _, err := f.Write(b); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", func() {}, err
	}
	f.Close()
	return f.Name(), func() { os.Remove(f.Name()) }, nil
}

func defaultImages() map[string]string {
	img := getenvDefault("QUILL_PIPELINE_UBUNTU_IMAGE", "catthehacker/ubuntu:act-latest")
	return map[string]string{
		"ubuntu-latest": img,
		"ubuntu-24.04":  img,
		"ubuntu-22.04":  img,
		"ubuntu-20.04":  img,
		"ubuntu":        img,
	}
}

func containerArch() string {
	if a := strings.TrimSpace(os.Getenv("QUILL_PIPELINE_CONTAINER_ARCH")); a != "" {
		return a
	}
	return "linux/" + runtime.GOARCH
}

func pipelineTimeout() time.Duration {
	if v := strings.TrimSpace(os.Getenv("QUILL_PIPELINE_TIMEOUT")); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			return d
		}
	}
	return 30 * time.Minute
}

func getenvDefault(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

func firstNonEmpty(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

func splitFullName(full string) (owner, name string, ok bool) {
	i := strings.IndexByte(full, '/')
	if i <= 0 || i == len(full)-1 {
		return "", "", false
	}
	return full[:i], full[i+1:], true
}

// workflowFileName gives act a stable file name for the single-workflow planner;
// the base name is enough since the content is supplied as a reader.
func workflowFileName(path string) string {
	if b := baseName(path); b != "" {
		return b
	}
	return "workflow.yml"
}

func baseName(path string) string {
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
