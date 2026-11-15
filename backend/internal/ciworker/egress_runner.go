package ciworker

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/nielsuitterdijk22/quill/internal/egress"
	"github.com/nielsuitterdijk22/quill/internal/pipeline"
)

// EgressApplier switches the instance's egress policy (egress.Controller).
type EgressApplier interface {
	Apply(pol *egress.Policy, onBlock func(host string, port int)) error
}

// EgressRunner enforces each job's egress policy around the inner runner:
// the policy is applied before the workflow starts (a job whose policy
// cannot be applied does not start — fail closed), refused hosts are
// reported live as they happen and summarised in the step that was running.
type EgressRunner struct {
	Inner pipeline.Runner
	Ctl   EgressApplier
	// AbortAfter stops the run once one host has been refused this many
	// times (0 = DefaultEgressAbortAfter; negative = never). A run's policy
	// is fixed when it is queued, so retrying a refused host cannot succeed:
	// a health-check loop against an unlisted host otherwise burns its whole
	// timeout (tempo's deploy waited 10 minutes on 40 refusals, 2026-11-15).
	AbortAfter int
}

// DefaultEgressAbortAfter is the refusal count of one host that stops a run.
// One-off probes (telemetry, update checks) stay well below it.
const DefaultEgressAbortAfter = 6

const egressLogPrefix = "quill egress: "

// Run implements pipeline.Runner.
func (r EgressRunner) Run(ctx context.Context, spec pipeline.JobSpec) (pipeline.RunResult, error) {
	var mu sync.Mutex
	blocked := map[string]int{}
	var curJob, curStep, aborted string
	sink := spec.LogSink
	limit := r.AbortAfter
	if limit == 0 {
		limit = DefaultEgressAbortAfter
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	onBlock := func(host string, port int) {
		key := host
		if key == "" {
			key = "(no hostname)"
		}
		key = fmt.Sprintf("%s:%d", key, port)
		mu.Lock()
		blocked[key]++
		first := blocked[key] == 1
		stop := limit > 0 && blocked[key] == limit && aborted == ""
		if stop {
			aborted = key
		}
		job, step := curJob, curStep
		mu.Unlock()
		if first && sink != nil {
			sink(job, step, egressLogPrefix+"blocked "+key+" (not on this repository's egress allowlist)")
		}
		if stop {
			if sink != nil {
				sink(job, step, egressLogPrefix+abortMessage(key, limit))
			}
			cancel()
		}
	}
	if err := r.Ctl.Apply(spec.Egress, onBlock); err != nil {
		return pipeline.RunResult{}, fmt.Errorf("egress controls could not be applied: %w", err)
	}
	if sink != nil {
		spec.LogSink = func(job, step, line string) {
			mu.Lock()
			curJob, curStep = job, step
			mu.Unlock()
			sink(job, step, line)
		}
	}
	res, err := r.Inner.Run(ctx, spec)
	mu.Lock()
	defer mu.Unlock()
	if aborted != "" {
		if err != nil {
			return res, fmt.Errorf("%s", abortMessage(aborted, limit))
		}
		res.Status = pipeline.StatusFailure
		markAbortedStep(&res)
	}
	if err == nil && len(blocked) > 0 {
		appendEgressSummary(&res, blocked)
		if aborted != "" {
			appendToFailedStep(&res, egressLogPrefix+abortMessage(aborted, limit)+"\n")
		}
	}
	return res, err
}

func abortMessage(key string, n int) string {
	return fmt.Sprintf("stopped the run: %s was refused %d times. Retrying cannot succeed, because the egress policy is fixed for this run. Allow the host under the repository's settings → Pipeline egress, then re-run.", key, n)
}

// markAbortedStep fails the step the abort interrupted (act reports it as
// cancelled or leaves it running) so the run page points at it.
func markAbortedStep(res *pipeline.RunResult) {
	for ji := range res.Jobs {
		for si := range res.Jobs[ji].Steps {
			if res.Jobs[ji].Steps[si].Status == pipeline.StatusFailure {
				return
			}
		}
	}
	for ji := range res.Jobs {
		for si := range res.Jobs[ji].Steps {
			st := &res.Jobs[ji].Steps[si]
			if st.Status != pipeline.StatusSuccess && st.Status != pipeline.StatusSkipped {
				st.Status = pipeline.StatusFailure
				res.Jobs[ji].Status = pipeline.StatusFailure
				return
			}
		}
	}
}

func appendToFailedStep(res *pipeline.RunResult, text string) {
	if t := summaryTarget(res); t != nil {
		t.Logs += text
	}
}

// appendEgressSummary adds the refused hosts to the first failed step (or
// the last step when nothing failed), where someone reading why a job broke
// will look.
func appendEgressSummary(res *pipeline.RunResult, blocked map[string]int) {
	keys := make([]string, 0, len(blocked))
	for k := range blocked {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString("\n" + egressLogPrefix + "outbound connections were refused by the egress policy:\n")
	for _, k := range keys {
		fmt.Fprintf(&b, "  %s (%d×)\n", k, blocked[k])
	}
	b.WriteString("Add expected hosts under the repository's settings → Pipeline egress.\n")
	if target := summaryTarget(res); target != nil {
		target.Logs += b.String()
	}
}

// summaryTarget is the first failed step, or the last step when none failed.
func summaryTarget(res *pipeline.RunResult) *pipeline.StepResult {
	var target *pipeline.StepResult
	for ji := range res.Jobs {
		for si := range res.Jobs[ji].Steps {
			s := &res.Jobs[ji].Steps[si]
			if s.Status == pipeline.StatusFailure && target == nil {
				target = s
			}
		}
	}
	if target == nil {
		for ji := len(res.Jobs) - 1; ji >= 0 && target == nil; ji-- {
			if n := len(res.Jobs[ji].Steps); n > 0 {
				target = &res.Jobs[ji].Steps[n-1]
			}
		}
	}
	return target
}
