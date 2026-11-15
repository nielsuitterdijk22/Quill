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
}

const egressLogPrefix = "quill egress: "

// Run implements pipeline.Runner.
func (r EgressRunner) Run(ctx context.Context, spec pipeline.JobSpec) (pipeline.RunResult, error) {
	var mu sync.Mutex
	blocked := map[string]int{}
	var curJob, curStep string
	sink := spec.LogSink
	onBlock := func(host string, port int) {
		key := host
		if key == "" {
			key = "(no hostname)"
		}
		key = fmt.Sprintf("%s:%d", key, port)
		mu.Lock()
		blocked[key]++
		first := blocked[key] == 1
		job, step := curJob, curStep
		mu.Unlock()
		if first && sink != nil {
			sink(job, step, egressLogPrefix+"blocked "+key+" (not on this repository's egress allowlist)")
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
	if err == nil && len(blocked) > 0 {
		appendEgressSummary(&res, blocked)
	}
	return res, err
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
	b.WriteString("Add expected hosts under the repository's Settings → Pipeline egress.\n")
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
	if target != nil {
		target.Logs += b.String()
	}
}
