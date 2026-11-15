package pipeline

import "testing"

func TestWorkflowEnvironment(t *testing.T) {
	cases := []struct {
		yaml, want string
		err        bool
	}{
		{"jobs:\n  a:\n    runs-on: x\n", "", false},
		{"jobs:\n  a:\n    environment: production\n  b:\n    runs-on: x\n", "production", false},
		{"jobs:\n  a:\n    environment:\n      name: staging\n      url: https://x\n", "staging", false},
		{"jobs:\n  a:\n    environment: production\n  b:\n    environment: production\n", "production", false},
		{"jobs:\n  a:\n    environment: production\n  b:\n    environment: staging\n", "", true},
		{"jobs:\n  a:\n    environment: ${{ inputs.env }}\n", "", true},
		{"jobs: [", "", true},
	}
	for _, c := range cases {
		got, err := WorkflowEnvironment(c.yaml)
		if got != c.want || (err != nil) != c.err {
			t.Errorf("WorkflowEnvironment(%q) = %q, %v", c.yaml, got, err)
		}
	}
}

func TestHasConcurrency(t *testing.T) {
	for y, want := range map[string]bool{
		"concurrency: deploy\njobs: {}\n":                      true,
		"concurrency:\n  group: ${{ github.ref }}\njobs: {}\n": true,
		"jobs:\n  a:\n    concurrency: x\n":                    false, // job-level only
		"jobs: {}\n":                                           false,
	} {
		if HasConcurrency(y) != want {
			t.Errorf("HasConcurrency(%q) != %v", y, want)
		}
	}
}

func TestKeepsSupersededRuns(t *testing.T) {
	for y, want := range map[string]bool{
		"on: push\n": false,
		"env:\n  QUILL_KEEP_SUPERSEDED_RUNS: \"true\"\n": true,
		"env:\n  QUILL_KEEP_SUPERSEDED_RUNS: true\n":     true,
		"env:\n  QUILL_KEEP_SUPERSEDED_RUNS: \"no\"\n":   false,
		"env:\n  OTHER: \"true\"\n":                      false,
	} {
		if got := KeepsSupersededRuns(y); got != want {
			t.Errorf("KeepsSupersededRuns(%q) = %v, want %v", y, got, want)
		}
	}
}
