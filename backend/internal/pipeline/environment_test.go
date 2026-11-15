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
