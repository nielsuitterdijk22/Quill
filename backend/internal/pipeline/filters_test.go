package pipeline

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

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

func TestMatchesPullRequest(t *testing.T) {
	wf := func(on string) string {
		return "name: t\non:\n" + on + "\njobs:\n  j:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo\n"
	}
	cases := []struct {
		name   string
		on     string
		base   string
		action string
		want   bool
	}{
		{"bare event, opened", "  pull_request:", "main", "opened", true},
		{"list form", "  [pull_request]", "main", "synchronize", true},
		{"inline list in on", "  push:\n  pull_request:\n    branches: [main]", "main", "opened", true},
		{"default types include reopened", "  pull_request:", "main", "reopened", true},
		{"default types exclude closed", "  pull_request:", "main", "closed", false},
		{"explicit types limit it", "  pull_request:\n    types: [opened]", "main", "synchronize", false},
		{"explicit types allow it", "  pull_request:\n    types: [opened, labeled]", "main", "opened", true},
		{"branches match the BASE branch", "  pull_request:\n    branches: [main]", "main", "opened", true},
		{"branches reject another base", "  pull_request:\n    branches: [main]", "develop", "opened", false},
		{"glob patterns", "  pull_request:\n    branches: ['release/**']", "release/1.2", "opened", true},
		{"branches-ignore excludes", "  pull_request:\n    branches-ignore: [wip]", "wip", "opened", false},
		{"branches-ignore allows others", "  pull_request:\n    branches-ignore: [wip]", "main", "opened", true},
		{"full ref is accepted as base", "  pull_request:\n    branches: [main]", "refs/heads/main", "opened", true},
		{"push-only workflow never matches", "  push:", "main", "opened", false},
	}
	for _, c := range cases {
		got, err := MatchesPullRequest(wf(c.on), c.base, c.action)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
	if _, err := MatchesPullRequest("on: [", "main", "opened"); err == nil {
		t.Error("malformed YAML must error")
	}
}

func TestEventFileCarriesPullRequest(t *testing.T) {
	path, cleanup, err := writeEventFile(JobSpec{
		Ref: "refs/heads/feat/x", CommitSHA: "abc123", RepoFullName: "acme/app",
		PullRequest: &PullRequestInfo{Number: 7, BaseRef: "main", HeadRef: "feat/x", Action: "synchronize"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	raw, _ := os.ReadFile(path)
	var ev map[string]any
	if err := json.Unmarshal(raw, &ev); err != nil {
		t.Fatal(err)
	}
	pr, _ := ev["pull_request"].(map[string]any)
	head, _ := pr["head"].(map[string]any)
	base, _ := pr["base"].(map[string]any)
	if ev["action"] != "synchronize" || ev["number"] != float64(7) || pr["number"] != float64(7) ||
		head["ref"] != "feat/x" || head["sha"] != "abc123" || base["ref"] != "main" {
		t.Fatalf("event = %s", raw)
	}
	// A push has no pull_request object.
	path2, cleanup2, _ := writeEventFile(JobSpec{Ref: "refs/heads/main", CommitSHA: "def"})
	defer cleanup2()
	raw2, _ := os.ReadFile(path2)
	if strings.Contains(string(raw2), "pull_request") {
		t.Fatalf("push event carries a pull_request: %s", raw2)
	}
}
