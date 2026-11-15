package live

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

// TestDeployGate drives environment-policy enforcement on production
// (2026-11-15): a workflow job declaring `environment:` is gated by the
// environment's policies before it is queued — here "only main may deploy".
// A run from another branch is refused (failed, with the reason on the run);
// the same workflow on main runs to success.
func TestDeployGate(t *testing.T) {
	requireLive(t)
	token, proj, repo := setupProjectRepo(t, "gate")
	env := fmt.Sprintf("e2e-gate-%d", time.Now().Unix()%100000)
	if st, b := apiCall(t, token, http.MethodPost, "/projects/"+proj+"/environments", map[string]any{"slug": env, "name": env}); st != 200 && st != 201 {
		t.Fatalf("create environment: %d %s", st, b)
	}
	t.Cleanup(func() { apiCall(t, token, http.MethodDelete, "/projects/"+proj+"/environments/"+env, nil) })
	rb := "/projects/" + proj + "/repos/" + repo
	if st, b := apiCall(t, token, http.MethodPut, rb+"/environment-policies", map[string]any{
		"pattern": env, "allowedSourceBranches": []string{"main"},
	}); st != 200 {
		t.Fatalf("environment policy: %d %s", st, b)
	}

	const wf = ".github/workflows/gated.yml"
	content := "on: [workflow_dispatch]\njobs:\n  deploy:\n    runs-on: ubuntu-latest\n    environment: " + env + "\n    steps:\n      - run: echo deployed\n"
	pushBranch(t, token, proj, repo, "main", wf, content)
	pushBranch(t, token, proj, repo, "feature", wf, content)

	trigger := func(ref string) int {
		st, b := apiCall(t, token, http.MethodPost, rb+"/pipelines", map[string]any{"workflow": wf, "ref": ref})
		if st != 200 && st != 201 {
			t.Fatalf("trigger %s: %d %s", ref, st, b)
		}
		r, _ := jbody(t, b)["run"].(map[string]any)
		n, _ := r["runNumber"].(float64)
		return int(n)
	}
	get := func(num int) map[string]any {
		st, b := apiCall(t, token, http.MethodGet, fmt.Sprintf("%s/pipelines/runs/%d?workflow=%s", rb, num, wf), nil)
		if st != 200 {
			t.Fatalf("get run %d: %d %s", num, st, b)
		}
		run := jbody(t, b)
		if inner, ok := run["run"].(map[string]any); ok {
			run = inner
		}
		return run
	}

	// Another branch: refused before it is queued.
	feature := get(trigger("feature"))
	reason, _ := feature["blockedReason"].(string)
	if feature["status"] != "failure" || !strings.Contains(reason, `ref "feature" may not deploy`) {
		t.Fatalf("feature deploy not blocked: status=%v reason=%q", feature["status"], reason)
	}
	t.Logf("feature run refused: %s", reason)

	// main: allowed, runs to success.
	num := trigger("main")
	start := time.Now()
	for time.Since(start) < 15*time.Minute {
		run := get(num)
		if run["blockedReason"] != nil && run["blockedReason"] != "" {
			t.Fatalf("main deploy blocked: %v", run["blockedReason"])
		}
		if run["environment"] != env {
			t.Fatalf("main run environment = %v, want %s", run["environment"], env)
		}
		switch run["status"] {
		case "success":
			t.Logf("main run deployed after %s", time.Since(start).Round(time.Second))
			return
		case "failure", "cancelled":
			t.Fatalf("main run %v", run["status"])
		}
		time.Sleep(10 * time.Second)
	}
	t.Fatal("main run did not finish")
}
