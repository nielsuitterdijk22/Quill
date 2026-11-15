package pipeline

import "testing"

func TestMatchesPushRef(t *testing.T) {
	cases := []struct {
		on, ref string
		want    bool
	}{
		{"on: push", "refs/heads/feature", true},
		{"on: [push, pull_request]", "refs/tags/v1", true},
		{"on:\n  push:\n", "refs/heads/x", true},
		{"on: pull_request", "refs/heads/main", false},
		{"on:\n  push:\n    branches: [main]", "refs/heads/main", true},
		{"on:\n  push:\n    branches: [main]", "refs/heads/agent/probe", false},
		{"on:\n  push:\n    branches: [main]", "refs/tags/v1", false}, // branch-only filter: tags don't trigger
		{"on:\n  push:\n    branches: ['release/*']", "refs/heads/release/1.0", true},
		{"on:\n  push:\n    branches: ['release/*']", "refs/heads/release/1/hot", false},
		{"on:\n  push:\n    branches: ['release/**']", "refs/heads/release/1/hot", true},
		{"on:\n  push:\n    branches: ['**', '!wip/**']", "refs/heads/wip/x", false},
		{"on:\n  push:\n    branches-ignore: ['dependabot/**']", "refs/heads/dependabot/npm", false},
		{"on:\n  push:\n    branches-ignore: ['dependabot/**']", "refs/heads/main", true},
		{"on:\n  push:\n    tags: ['v*']", "refs/tags/v1.2", true},
		{"on:\n  push:\n    tags: ['v*']", "refs/heads/main", false}, // tag-only filter: branches don't trigger
		{"on:\n  push:\n    branches: [main]\n    tags: ['v*']", "refs/tags/v2", true},
		{"on:\n  push:\n    paths: ['backend/**']", "refs/heads/any", true},
		{"on:\n  push:\n    branches: [main]\n  workflow_dispatch:", "refs/heads/feature", false},
		{"on:\n  push:\n    branches: main", "refs/heads/main", true},
	}
	for _, c := range cases {
		got, err := MatchesPushRef(c.on+"\njobs: {}\n", c.ref)
		if err != nil || got != c.want {
			t.Errorf("%q @ %s = %v (%v), want %v", c.on, c.ref, got, err, c.want)
		}
	}
}
