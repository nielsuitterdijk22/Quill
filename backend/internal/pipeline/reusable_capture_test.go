package pipeline

import (
	"strings"
	"testing"
	"time"

	"github.com/nektos/act/pkg/model"
)

// Reusable-workflow callers (`jobs.x.uses: ./wf.yml` or a cross-repo
// template) have no steps of their own: act runs the called workflow's jobs
// with their OWN jobID, tagging every entry with job = "<caller>/<workflow>/<inner>".
// Quill only assembled the outer plan's job ids, so a caller showed up as a
// failed job with a lone "setup" step even though act ran it successfully
// (its `needs:` dependents ran) — found by the live pipeline showcase.
func TestAssembleFoldsReusableWorkflowJobsIntoCaller(t *testing.T) {
	const wf = `name: ci
on: [push]
jobs:
  unit:
    uses: ./.github/workflows/test.yml
    with:
      suite: unit
  build:
    needs: [unit]
    runs-on: ubuntu-latest
    steps:
      - run: echo build
`
	planner, err := model.NewSingleWorkflowPlanner("ci.yml", strings.NewReader(wf))
	if err != nil {
		t.Fatal(err)
	}
	plan, err := planner.PlanEvent("push")
	if err != nil {
		t.Fatal(err)
	}

	c := newLogCapture(nil)
	log := c.WithJobLogger()
	// The caller's own job-level chatter (no steps, no jobResult of its own).
	log.WithField("jobID", "unit").WithField("job", "ci/unit").Info("resolving reusable workflow")
	// The called workflow's inner job "run", padded like act pads job names.
	inner := log.WithField("jobID", "run").WithField("job", "unit/reusable-tests/run   ")
	st := inner.WithField("stepID", []string{"0"}).WithField("step", "run suite")
	st.WithField("raw_output", true).Info("running the unit suite")
	st.WithField("stepResult", model.StepStatusSuccess).Info("✅ Success")
	inner.WithField("jobResult", "success").Info("🏁 Job succeeded")
	// The dependent job.
	b := log.WithField("jobID", "build").WithField("job", "ci/build")
	bs := b.WithField("stepID", []string{"0"}).WithField("step", "echo build")
	bs.WithField("raw_output", true).Info("build")
	bs.WithField("stepResult", model.StepStatusSuccess).Info("✅ Success")
	b.WithField("jobResult", "success").Info("🏁 Job succeeded")

	res := (&actRunner{maxLogBytes: 1 << 20}).assemble(plan, c, time.Now())
	if res.Status != StatusSuccess {
		t.Fatalf("run status = %s, want success; jobs=%+v", res.Status, res.Jobs)
	}
	var unit *JobResult
	for i := range res.Jobs {
		if res.Jobs[i].Key == "unit" {
			unit = &res.Jobs[i]
		}
	}
	if unit == nil {
		t.Fatal("caller job missing")
	}
	if unit.Status != StatusSuccess {
		t.Fatalf("caller status = %s, want success (steps %+v)", unit.Status, unit.Steps)
	}
	found := false
	for _, s := range unit.Steps {
		if strings.Contains(s.Name, "run suite") && strings.Contains(s.Logs, "running the unit suite") {
			found = true
		}
	}
	if !found {
		t.Fatalf("inner job steps not folded into the caller: %+v", unit.Steps)
	}
	// The inner job must not ALSO appear as a separate top-level job.
	for _, j := range res.Jobs {
		if j.Key == "run" {
			t.Fatal("inner reusable job leaked as a top-level job")
		}
	}

	// A failing inner job fails its caller.
	c2 := newLogCapture(nil)
	l2 := c2.WithJobLogger()
	in2 := l2.WithField("jobID", "run").WithField("job", "unit/reusable-tests/run")
	s2 := in2.WithField("stepID", []string{"0"}).WithField("step", "run suite")
	s2.WithField("stepResult", model.StepStatusFailure).Info("❌ Failure")
	in2.WithField("jobResult", "failure").Info("🏁 Job failed")
	res2 := (&actRunner{maxLogBytes: 1 << 20}).assemble(plan, c2, time.Now())
	for _, j := range res2.Jobs {
		if j.Key == "unit" && j.Status != StatusFailure {
			t.Fatalf("failing inner job: caller status = %s", j.Status)
		}
	}
	if res2.Status != StatusFailure {
		t.Fatalf("run with a failing reusable job = %s", res2.Status)
	}
}

// A job whose only captured output is job-level chatter but whose act result
// is success must not be reported as failed (the synthetic "setup" step read
// the job status before it was computed).
func TestAssembleSetupStepOfSuccessfulJob(t *testing.T) {
	const wf = "name: w\non: [push]\njobs:\n  a:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo a\n"
	planner, _ := model.NewSingleWorkflowPlanner("w.yml", strings.NewReader(wf))
	plan, _ := planner.PlanEvent("push")
	c := newLogCapture(nil)
	j := c.WithJobLogger().WithField("jobID", "a").WithField("job", "w/a")
	j.Info("pulling image")
	s := j.WithField("stepID", []string{"0"}).WithField("step", "echo a")
	s.WithField("stepResult", model.StepStatusSuccess).Info("ok")
	j.WithField("jobResult", "success").Info("🏁 Job succeeded")
	res := (&actRunner{maxLogBytes: 1 << 20}).assemble(plan, c, time.Now())
	if res.Jobs[0].Steps[0].Name != "setup" || res.Jobs[0].Steps[0].Status != StatusSuccess {
		t.Fatalf("setup step = %+v", res.Jobs[0].Steps[0])
	}
}
