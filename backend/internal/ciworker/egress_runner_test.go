package ciworker

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/nielsuitterdijk22/quill/internal/egress"
	"github.com/nielsuitterdijk22/quill/internal/pipeline"
)

type fakeEgress struct {
	applied []*egress.Policy
	onBlock func(string, int)
	err     error
}

func (f *fakeEgress) Apply(p *egress.Policy, cb func(string, int)) error {
	f.applied = append(f.applied, p)
	f.onBlock = cb
	return f.err
}

type blockingRunner struct{ ctl *fakeEgress }

func (r blockingRunner) Run(ctx context.Context, spec pipeline.JobSpec) (pipeline.RunResult, error) {
	spec.LogSink("build", "npm ci", "fetching")
	r.ctl.onBlock("evil.example", 443)
	r.ctl.onBlock("evil.example", 443)
	return pipeline.RunResult{Status: pipeline.StatusFailure, Jobs: []pipeline.JobResult{{Key: "build", Steps: []pipeline.StepResult{
		{Name: "checkout", Status: pipeline.StatusSuccess},
		{Name: "npm ci", Status: pipeline.StatusFailure, Logs: "ECONNRESET"},
	}}}}, nil
}

// Each job's policy is applied before it runs; refused hosts are reported
// live once (on the running step) and summarised in the failed step; a
// policy that cannot be applied stops the job (fail closed).
func TestEgressRunner(t *testing.T) {
	ctl := &fakeEgress{}
	var live []string
	pol := &egress.Policy{Mode: egress.ModeAllowlist, Hosts: []string{"registry.npmjs.org"}}
	res, err := EgressRunner{Inner: blockingRunner{ctl}, Ctl: ctl}.Run(context.Background(), pipeline.JobSpec{
		Egress:  pol,
		LogSink: func(job, step, line string) { live = append(live, job+"/"+step+": "+line) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(ctl.applied) != 1 || ctl.applied[0] != pol {
		t.Fatalf("policy not applied: %+v", ctl.applied)
	}
	want := "build/npm ci: quill egress: blocked evil.example:443 (not on this repository's egress allowlist)"
	n := 0
	for _, l := range live {
		if l == want {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("live block lines = %d in %q", n, live)
	}
	logs := res.Jobs[0].Steps[1].Logs
	if !strings.Contains(logs, "evil.example:443 (2×)") || !strings.HasPrefix(logs, "ECONNRESET") {
		t.Fatalf("failed step logs: %q", logs)
	}
	if res.Jobs[0].Steps[0].Logs != "" {
		t.Fatal("summary landed on the wrong step")
	}

	ctl.err = errors.New("iptables: permission denied")
	if _, err := (EgressRunner{Inner: blockingRunner{ctl}, Ctl: ctl}).Run(context.Background(), pipeline.JobSpec{Egress: pol}); err == nil {
		t.Fatal("job ran without its egress policy")
	}
}

// retryingRunner is a health-check loop against a refused host: it retries
// until its context is cancelled, then reports the step act was running.
type retryingRunner struct {
	ctl   *fakeEgress
	tries *int
}

func (r retryingRunner) Run(ctx context.Context, spec pipeline.JobSpec) (pipeline.RunResult, error) {
	if spec.LogSink != nil {
		spec.LogSink("deploy", "Health check", "waiting for the api")
	}
	for ctx.Err() == nil && *r.tries < 1000 {
		*r.tries++
		r.ctl.onBlock("api.example", 443)
	}
	return pipeline.RunResult{Status: pipeline.StatusCancelled, Jobs: []pipeline.JobResult{{Key: "deploy", Status: pipeline.StatusCancelled, Steps: []pipeline.StepResult{
		{Name: "Build", Status: pipeline.StatusSuccess},
		{Name: "Health check", Status: pipeline.StatusCancelled, Logs: "curl: (35) SSL_ERROR_SYSCALL"},
	}}}}, nil
}

// Retrying a refused host can't succeed (the policy is fixed for the run):
// after AbortAfter refusals of one host the run stops, failed, saying why,
// instead of burning the step's own timeout.
func TestEgressRunnerStopsRetryLoops(t *testing.T) {
	ctl := &fakeEgress{}
	tries := 0
	var live []string
	res, err := EgressRunner{Inner: retryingRunner{ctl, &tries}, Ctl: ctl}.Run(context.Background(), pipeline.JobSpec{
		Egress:  &egress.Policy{Mode: egress.ModeAllowlist},
		LogSink: func(job, step, line string) { live = append(live, line) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if tries != DefaultEgressAbortAfter {
		t.Fatalf("run kept going: %d refusals", tries)
	}
	if res.Status != pipeline.StatusFailure || res.Jobs[0].Steps[1].Status != pipeline.StatusFailure {
		t.Fatalf("aborted run not failed: %+v", res)
	}
	logs := res.Jobs[0].Steps[1].Logs
	if !strings.Contains(logs, "stopped the run: api.example:443 was refused 6 times") || !strings.Contains(logs, "api.example:443 (6×)") {
		t.Fatalf("step logs: %q", logs)
	}
	if !strings.Contains(strings.Join(live, "\n"), "stopped the run") {
		t.Fatalf("abort not reported live: %q", live)
	}

	// Disabled: the loop runs its course.
	tries = 0
	if _, err := (EgressRunner{Inner: retryingRunner{ctl, &tries}, Ctl: ctl, AbortAfter: -1}).Run(context.Background(), pipeline.JobSpec{}); err != nil || tries != 1000 {
		t.Fatalf("AbortAfter=-1: tries=%d err=%v", tries, err)
	}
}
