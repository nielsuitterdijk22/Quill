package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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

	// Action cache dir: act fetches remote actions (actions/checkout,
	// actions/setup-go, ...) into rc.ActionCacheDir, which defaults to
	// ~/.cache/act. The backend image creates its user with -H (no home), so
	// that default is unwritable there and every uses: step errored in its
	// pre-step silently - the job failed with all steps "skipped" and nothing
	// in the logs. Pin a temp dir we mkdir ourselves (override with
	// QUILL_PIPELINE_ACTION_CACHE).
	actionCacheDir := filepath.Join(os.TempDir(), "quill-act-cache")
	if d := strings.TrimSpace(os.Getenv("QUILL_PIPELINE_ACTION_CACHE")); d != "" {
		actionCacheDir = d
	}
	_ = os.MkdirAll(actionCacheDir, 0o755)

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
		Env:                   githubURLOverrides(spec.ServerURL),
		Secrets:               mapOrEmpty(spec.Secrets),
		Vars:                  map[string]string{},
		GitHubInstance:        "github.com",
		ActionCacheDir:        actionCacheDir,
		ActionCache:           newActionSource(&runner.GoGitActionCache{Path: actionCacheDir}, spec.ServerURL),
		// One job at a time: the dispatcher shares the VM with forgejo,
		// zitadel and the databases, and two concurrent builds (go + next)
		// have wedged the entire box - ssh banners timed out, auth/git down
		// for the duration (2026-11-11). Serializing halves the memory peak;
		// runs get slower on multi-job workflows but the platform stays up.
		ConcurrentJobs: 1,
	}
	rn, err := runner.New(cfg)
	if err != nil {
		return RunResult{}, fmt.Errorf("init runner: %w", err)
	}

	cap := newLogCapture(spec.LogSink)
	exec := rn.NewPlanExecutor(plan)

	runCtx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	runCtx = runner.WithJobLoggerFactory(runCtx, cap)

	execErr := exec(runCtx)

	result := r.assemble(plan, cap, started)
	if execErr != nil {
		explainFailure(&result, execErr, spec.LogSink)
	}
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
			// A reusable-workflow caller has no steps of its own: its work is
			// the called workflow's jobs, captured under the caller's name.
			inner := cap.reusableJobs(jr.Name)
			if jc == nil && len(inner) == 0 {
				jr.Status = StatusSkipped
				res.Jobs = append(res.Jobs, jr)
				continue
			}
			if jc == nil {
				jc = &jobCapture{steps: map[string]*stepCapture{}, started: inner[0].started}
			}
			jr.StartedAt = jc.started
			jr.FinishedAt = jc.finished
			setupIdx := -1
			// A failed job with no steps means act couldn't even start it
			// (container/network/image errors). Surface the job-level lines
			// as a synthetic "setup" step so the run explains itself in the
			// UI and the stored logs. Its status is settled below, once the
			// job's own status is known.
			if len(jc.setupLogs.Bytes()) > 0 {
				jr.Steps = append(jr.Steps, StepResult{
					Name:       "setup",
					Type:       StepTypeRun,
					Logs:       clampLog(jc.setupLogs.String(), r.maxLogBytes),
					StartedAt:  jc.started,
					FinishedAt: jc.finished,
				})
				setupIdx = len(jr.Steps) - 1
			}
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
			innerFailed := false
			for _, ij := range inner {
				prefix := ij.name + " · "
				if ij.setupLogs.Len() > 0 {
					st := StatusSuccess
					if ij.result == "failure" && len(ij.order) == 0 {
						st = StatusFailure
					}
					jr.Steps = append(jr.Steps, StepResult{
						Name: prefix + "setup", Type: StepTypeRun, Status: st,
						Logs:      clampLog(ij.setupLogs.String(), r.maxLogBytes),
						StartedAt: ij.started, FinishedAt: ij.finished,
					})
				}
				for _, sid := range ij.order {
					sc := ij.steps[sid]
					jr.Steps = append(jr.Steps, StepResult{
						Name:      prefix + firstNonEmpty(sc.name, sid),
						Type:      StepTypeRun,
						Status:    mapStatus(sc.outcome),
						Logs:      clampLog(sc.logs.String(), r.maxLogBytes),
						StartedAt: sc.started, FinishedAt: sc.finished,
					})
				}
				if ij.result == "failure" {
					innerFailed = true
				}
				if ij.finished.After(jr.FinishedAt) {
					jr.FinishedAt = ij.finished
				}
			}
			result := jc.result
			if len(inner) > 0 && result == "" {
				// The caller's outcome is its called jobs' outcome.
				result = "success"
				if innerFailed {
					result = "failure"
				}
			}
			if setupIdx >= 0 {
				// Exclude the setup step itself from the roll-up it feeds.
				jr.Steps[setupIdx].Status = StatusSuccess
			}
			jr.Status = mapJobStatus(result, jr.Steps)
			if setupIdx >= 0 && jr.Status == StatusFailure && len(jr.Steps) == 1 {
				// Nothing but job-level output: the setup lines ARE the failure.
				jr.Steps[setupIdx].Status = StatusFailure
			}
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

// githubURLOverrides maps the git instance's base URL to the github-context env
// act reads when resolving remote references: GITHUB_SERVER_URL drives git
// fetches of remote actions and reusable workflows (act overwrites the
// otherwise-hardcoded github.com with the github context ServerURL) and
// GITHUB_API_URL drives API-style actions; Forgejo serves its API at /api/v1.
// An empty URL yields act's GitHub defaults (nil map — no overrides).
func githubURLOverrides(serverURL string) map[string]string {
	base := strings.TrimSuffix(strings.TrimSpace(serverURL), "/")
	if base == "" {
		return nil
	}
	return map[string]string{
		"GITHUB_SERVER_URL": base,
		"GITHUB_API_URL":    base + "/api/v1",
	}
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

// mapOrEmpty returns m, or a fresh empty map when m is nil, so act always
// receives a non-nil secrets map.
func mapOrEmpty(m map[string]string) map[string]string {
	if m == nil {
		return map[string]string{}
	}
	return m
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

// explainFailure puts the executor's error into the run. act reports errors
// raised outside a step's main stage (fetching a `uses:` action, a pre-step)
// only through its return value, so a job could fail with every step
// "skipped" and nothing in the logs explaining why. The error lands at the
// end of each failed job's setup step (created when missing) and in the live
// feed.
func explainFailure(res *RunResult, execErr error, sink LogSink) {
	msg := "Error: " + strings.TrimSpace(execErr.Error())
	for i := range res.Jobs {
		jr := &res.Jobs[i]
		if jr.Status != StatusFailure {
			continue
		}
		idx := -1
		for j, st := range jr.Steps {
			if st.Name == "setup" {
				idx = j
				break
			}
		}
		if idx < 0 {
			jr.Steps = append([]StepResult{{Name: "setup", Type: StepTypeRun, Status: StatusSuccess, StartedAt: jr.StartedAt, FinishedAt: jr.FinishedAt}}, jr.Steps...)
			idx = 0
		}
		st := &jr.Steps[idx]
		if st.Logs != "" && !strings.HasSuffix(st.Logs, "\n") {
			st.Logs += "\n"
		}
		st.Logs += msg + "\n"
		// Nothing after setup ran: the setup stage is where it failed.
		ran := false
		for j, other := range jr.Steps {
			if j != idx && other.Status != StatusSkipped && other.Status != "" {
				ran = true
			}
		}
		if !ran {
			st.Status = StatusFailure
		}
		if sink != nil {
			sink(jr.Key, "setup", msg+"\n")
		}
	}
}
