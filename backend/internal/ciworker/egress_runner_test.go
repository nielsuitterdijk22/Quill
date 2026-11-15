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
