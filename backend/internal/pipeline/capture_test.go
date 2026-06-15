package pipeline

import (
	"testing"

	"github.com/nektos/act/pkg/model"
)

// TestLogCaptureRoutesEntries verifies the hook groups act's log entries into the
// right job/step buckets, keeps raw_output as logs, and records final outcomes —
// the logic that turns act's stream into a structured result, without Docker.
func TestLogCaptureRoutesEntries(t *testing.T) {
	c := newLogCapture()
	log := c.WithJobLogger()

	je := log.WithField("jobID", "build")
	se := je.WithField("stepID", []string{"step-0"}).WithField("step", "Run echo")
	se.WithField("raw_output", true).Info("hello-world")
	se.WithField("stepResult", model.StepStatusSuccess).Info("✅ Success")
	je.WithField("jobResult", "success").Info("🏁 Job succeeded")

	jc := c.job("build")
	if jc == nil {
		t.Fatal("job 'build' not captured")
	}
	if jc.result != "success" {
		t.Fatalf("job result = %q, want success", jc.result)
	}
	if len(jc.order) != 1 || jc.order[0] != "step-0" {
		t.Fatalf("step order = %v, want [step-0]", jc.order)
	}
	sc := jc.steps["step-0"]
	if sc.name != "Run echo" {
		t.Fatalf("step name = %q, want 'Run echo'", sc.name)
	}
	if sc.outcome != "success" {
		t.Fatalf("step outcome = %q, want success", sc.outcome)
	}
	if got := sc.logs.String(); got != "hello-world\n" {
		t.Fatalf("step logs = %q, want \"hello-world\\n\"", got)
	}
}

// model.StepStatusSuccess is referenced above to mirror act's field type.

// TestLogCaptureIgnoresEntriesWithoutJob ensures stray entries (no jobID) don't
// create phantom jobs.
func TestLogCaptureIgnoresEntriesWithoutJob(t *testing.T) {
	c := newLogCapture()
	c.WithJobLogger().WithField("note", "x").Info("ignored")
	if len(c.jobs) != 0 {
		t.Fatalf("captured %d jobs, want 0", len(c.jobs))
	}
}
