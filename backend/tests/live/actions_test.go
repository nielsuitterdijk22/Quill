package live

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

// TestRemoteStepActions: a workflow using the everyday GitHub actions
// (actions/checkout, actions/setup-go) runs green on Quill CI. Before
// 2026-11-14 act resolved them against Quill's own git host (no Forgejo copy
// any more), so every such job failed in setup with all steps "skipped".
func TestRemoteStepActions(t *testing.T) {
	requireLive(t)
	token, proj, repo := setupProjectRepo(t, "actions")
	pushBranch(t, token, proj, repo, "main", "go.mod", "module example.com/hello\n\ngo 1.22\n")
	pushBranch(t, token, proj, repo, "main", "main.go",
		"package main\n\nimport \"fmt\"\n\nfunc main() { fmt.Println(\"hello-from-setup-go\") }\n")
	const wf = ".github/workflows/ci.yml"
	pushBranch(t, token, proj, repo, "main", wf, `on: [workflow_dispatch]
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: "1.22"
      - run: go run .
`)
	rp := "/projects/" + proj + "/repos/" + repo + "/pipelines"
	st, b := apiCall(t, token, http.MethodPost, rp, map[string]any{"workflow": wf, "ref": "main"})
	if st != 200 && st != 201 {
		t.Fatalf("trigger: %d %s", st, b)
	}
	num := 0
	if r, ok := jbody(t, b)["run"].(map[string]any); ok {
		if f, ok := r["runNumber"].(float64); ok {
			num = int(f)
		}
	}
	if num == 0 {
		t.Fatalf("no run number: %s", b)
	}
	start := time.Now()
	status := ""
	for time.Since(start) < 15*time.Minute {
		st, b = apiCall(t, token, http.MethodGet, fmt.Sprintf("%s/runs/%d?workflow=%s", rp, num, wf), nil)
		if st == 200 {
			run := jbody(t, b)
			if inner, ok := run["run"].(map[string]any); ok {
				run = inner
			}
			s, _ := run["status"].(string)
			if s != status {
				t.Logf("run %d status=%s after %s", num, s, time.Since(start).Round(time.Second))
				status = s
			}
			if s == "success" || s == "failure" || s == "failed" || s == "cancelled" {
				break
			}
		}
		time.Sleep(10 * time.Second)
	}
	st, logs := apiCall(t, token, http.MethodGet, fmt.Sprintf("%s/runs/%d/logs?workflow=%s", rp, num, wf), nil)
	if status != "success" {
		tail := string(logs)
		if len(tail) > 3000 {
			tail = tail[len(tail)-3000:]
		}
		t.Fatalf("run %d ended %q after %s; logs (%d):\n%s", num, status, time.Since(start).Round(time.Second), st, redactTokens(tail))
	}
	if !strings.Contains(string(logs), "hello-from-setup-go") {
		t.Fatalf("program output missing from logs: %.600s", logs)
	}
}
