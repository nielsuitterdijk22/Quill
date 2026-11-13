package live

import (
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

// TestPipelineQueueRoundTrip (serverless Phase A): with queue mode on, a
// triggered run is enqueued, the broker boots an ephemeral CI instance, the
// ciworker claims + executes it and reports back. Asserts the run SUCCEEDS and
// its step log carries the echo. Deadline covers a cold instance boot (docker
// install + image pull); override with QUILL_E2E_QUEUE_DEADLINE (e.g. 15m).
func TestPipelineQueueRoundTrip(t *testing.T) {
	requireLive(t)
	deadlineDur := 12 * time.Minute
	if v := os.Getenv("QUILL_E2E_QUEUE_DEADLINE"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			deadlineDur = d
		}
	}
	token, proj, repo := setupProjectRepo(t, "queue")
	pushBranch(t, token, proj, repo, "main", "a.txt", "a\n")
	pushBranch(t, token, proj, repo, "main", ".github/workflows/ci.yml",
		"on: [push]\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo queue-roundtrip-ok\n")
	rp := "/projects/" + proj + "/repos/" + repo + "/pipelines"

	st, b := apiCall(t, token, http.MethodPost, rp, map[string]any{"workflow": ".github/workflows/ci.yml", "ref": "main"})
	if st != 200 && st != 201 {
		t.Fatalf("trigger: %d %s", st, b)
	}
	var num int
	st, b = apiCall(t, token, http.MethodGet, rp+"/runs", nil)
	if st == 200 {
		if arr, ok := jbody(t, b)["runs"].([]any); ok && len(arr) > 0 {
			if f, ok := arr[0].(map[string]any)["runNumber"].(float64); ok {
				num = int(f)
			} else if f, ok := arr[0].(map[string]any)["number"].(float64); ok {
				num = int(f)
			}
		}
	}
	if num == 0 {
		t.Fatalf("no run listed: %s", b)
	}

	start := time.Now()
	status := ""
	for time.Since(start) < deadlineDur {
		st, b = apiCall(t, token, http.MethodGet, fmt.Sprintf("%s/runs/%d?workflow=.github/workflows/ci.yml", rp, num), nil)
		if st == 200 {
			run := jbody(t, b)
			if inner, ok := run["run"].(map[string]any); ok {
				run = inner
			}
			s, _ := run["status"].(string)
			if s != status {
				fmt.Printf("RUN %d status=%s after %s\n", num, s, time.Since(start).Round(time.Second))
				status = s
			}
			if s == "success" || s == "failed" || s == "cancelled" {
				break
			}
		}
		time.Sleep(10 * time.Second)
	}
	if status != "success" {
		t.Fatalf("run %d ended %q after %s", num, status, time.Since(start).Round(time.Second))
	}
	st, b = apiCall(t, token, http.MethodGet, fmt.Sprintf("%s/runs/%d/logs?workflow=.github/workflows/ci.yml", rp, num), nil)
	if st != 200 || !strings.Contains(string(b), "queue-roundtrip-ok") {
		t.Fatalf("logs: %d %.400s", st, b)
	}
}
