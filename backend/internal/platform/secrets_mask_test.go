package platform

import (
	"strings"
	"testing"

	"github.com/nielsuitterdijk22/quill/internal/pipeline"
)

func TestSecretMaskRedactsValues(t *testing.T) {
	m := newSecretMasker(map[string]string{
		"TOKEN": "supersecret",
		"SHORT": "ab", // below minMaskLen, must not be masked
	})
	got := m.mask("using supersecret to auth; ab stays")
	if strings.Contains(got, "supersecret") {
		t.Fatalf("secret leaked: %q", got)
	}
	if !strings.Contains(got, maskReplacement) {
		t.Fatalf("expected redaction marker: %q", got)
	}
	if !strings.Contains(got, "ab stays") {
		t.Fatalf("short value should not be masked: %q", got)
	}
}

func TestSecretMaskMultilineValue(t *testing.T) {
	// A multi-line secret is registered by its individual lines too, so a runner
	// that emits it one line at a time is still masked.
	m := newSecretMasker(map[string]string{"KEY": "line-one\nline-two"})
	got := m.mask("prefix line-one suffix")
	if strings.Contains(got, "line-one") {
		t.Fatalf("multiline secret line leaked: %q", got)
	}
}

func TestSecretMaskResult(t *testing.T) {
	m := newSecretMasker(map[string]string{"PW": "hunter2xx"})
	result := pipeline.RunResult{
		Jobs: []pipeline.JobResult{{
			Steps: []pipeline.StepResult{{Logs: "echo hunter2xx > out"}},
		}},
	}
	m.maskResult(&result)
	if strings.Contains(result.Jobs[0].Steps[0].Logs, "hunter2xx") {
		t.Fatalf("secret leaked in step logs: %q", result.Jobs[0].Steps[0].Logs)
	}
}

func TestSecretMaskEmptyIsNoop(t *testing.T) {
	m := newSecretMasker(nil)
	const line = "nothing to hide here"
	if got := m.mask(line); got != line {
		t.Fatalf("empty masker changed input: %q", got)
	}
}
