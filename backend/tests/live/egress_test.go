package live

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

// TestPipelineEgress runs a workflow on a real CI instance under the default
// allowlist plus one repository host (2026-11-15 egress controls): public
// registries and the repo's extra host are reachable, everything else —
// another HTTPS host, plain HTTP, a bare IP, SSH, and the same from a
// container on a Docker bridge network — is refused, and the run log names
// the refused host.
func TestPipelineEgress(t *testing.T) {
	requireLive(t)
	token, proj, repo := setupProjectRepo(t, "egress")

	st, b := apiCall(t, token, http.MethodGet, "/me/egress", nil)
	if st != 200 || !strings.Contains(string(b), `"mode":"allowlist"`) || !strings.Contains(string(b), "registry.npmjs.org") {
		t.Fatalf("GET /me/egress: %d %s", st, b)
	}
	ep := "/projects/" + proj + "/repos/" + repo + "/egress"
	if st, b = apiCall(t, token, http.MethodPut, ep, map[string]any{"hosts": []string{"10.0.0.1"}}); st != 400 {
		t.Fatalf("PUT invalid host: %d %s", st, b)
	}
	if st, b = apiCall(t, token, http.MethodPut, ep, map[string]any{"hosts": []string{"example.org"}}); st != 200 || !strings.Contains(string(b), `"example.org"`) {
		t.Fatalf("PUT repo hosts: %d %s", st, b)
	}

	const wf = ".github/workflows/egress.yml"
	pushBranch(t, token, proj, repo, "main", wf, `on: [workflow_dispatch]
jobs:
  probe:
    runs-on: ubuntu-latest
    steps:
      - name: egress probes
        run: |
          p() { code=$(curl -s -o /dev/null -m 10 -w '%{http_code}' "$2" || true); echo "probe:$1=$code"; }
          p npm https://registry.npmjs.org/
          p repo_extra https://example.org/
          p other_https https://example.com/
          p other_http http://example.com/
          p bare_ip https://1.1.1.1/
          if timeout 6 bash -c 'echo > /dev/tcp/github.com/22' 2>/dev/null; then echo "probe:ssh=open"; else echo "probe:ssh=closed"; fi
      - name: bridge network
        run: |
          curl -fsSL https://download.docker.com/linux/static/stable/x86_64/docker-27.3.1.tgz | tar -xz -C /tmp
          /tmp/docker/docker run --rm alpine:3.20 sh -c '
            if wget -q -T 8 -O /dev/null https://registry.npmjs.org/; then echo "probe:bridge_npm=open"; else echo "probe:bridge_npm=closed"; fi
            if wget -q -T 8 -O /dev/null https://example.com/; then echo "probe:bridge_other=open"; else echo "probe:bridge_other=closed"; fi'
`)
	rp := "/projects/" + proj + "/repos/" + repo + "/pipelines"
	st, b = apiCall(t, token, http.MethodPost, rp, map[string]any{"workflow": wf, "ref": "main"})
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
		"npm":          "200",
		"repo_extra":   "200",
		"other_https":  "000",
		"other_http":   "403",
		"bare_ip":      "000",
		"ssh":          "closed",
		"bridge_npm":   "open",
		"bridge_other": "closed",
	}
	for k, v := range want {
		if probes[k] != v {
			t.Errorf("probe %s = %q, want %q", k, probes[k], v)
		}
	}
	// The stored log carries the worker's summary of refused hosts.
	if !strings.Contains(string(logs), "refused by the egress policy") || !strings.Contains(string(logs), "example.com:443") {
		t.Error("run log does not name the refused host")
	}
}
