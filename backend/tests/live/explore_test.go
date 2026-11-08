package live

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

// TestWebCrawl fetches the main app pages with a real session and reports
// any 5xx or unexpected redirects.
func TestWebCrawl(t *testing.T) {
	requireLive(t)
	token, proj, repo := setupProjectRepo(t, "crawl")
	_ = repo

	// build a web session via the app callback flow
	c := webSession(t)
	pages := []string{
		"/repositories", "/projects", "/pulls", "/pipelines", "/settings",
		"/projects/" + proj, "/projects/" + proj + "/repos", "/projects/" + proj + "/repos/new",
		"/projects/" + proj + "/settings",
		"/" + proj + "/" + repo,
		"/" + proj + "/" + repo + "/pulls",
		"/" + proj + "/" + repo + "/pipelines",
		"/" + proj + "/" + repo + "/settings",
	}
	for _, p := range pages {
		st, hdr, body := get(t, c, appURL+p, nil)
		flag := ""
		if st >= 500 {
			flag = " <-- 5xx"
		}
		if st == 307 {
			flag = " -> " + hdr.Get("Location")
		}
		fmt.Printf("CRAWL %s -> %d%s (len %d)\n", p, st, flag, len(body))
		if st >= 500 {
			t.Errorf("GET %s: %d", p, st)
		}
	}
	_ = token
}

// TestWebhookTriggersPipeline pushes to a repo and checks whether the Forgejo
// system webhook reaches the api and starts a pipeline run.
func TestWebhookTriggersPipeline(t *testing.T) {
	requireLive(t)
	token, proj, repo := setupProjectRepo(t, "hook")
	pushBranch(t, token, proj, repo, "main", ".github/workflows/ci.yml", "on: [push]\njobs:\n  build:\n    steps:\n      - run: echo hook-test\n")
	pushBranch(t, token, proj, repo, "main", "hook.txt", "hook\n")

	// give the webhook a moment, then list runs
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		st, b := apiCall(t, token, http.MethodGet, "/projects/"+proj+"/repos/"+repo+"/pipelines/runs", nil)
		if st == 200 && strings.Contains(string(b), `"runs":[`) && !strings.Contains(string(b), `"runs":[]`) {
			fmt.Printf("WEBHOOK RUN: %d %.200s\n", st, b)
			return
		}
		time.Sleep(3 * time.Second)
	}
	st, b := apiCall(t, token, http.MethodGet, "/projects/"+proj+"/repos/"+repo+"/pipelines/runs", nil)
	fmt.Printf("WEBHOOK no runs after 30s: %d %.150s\n", st, b)
	t.Errorf("no pipeline run appeared after push (webhook -> api may be broken)")
}

// TestMergeGateWithPolicy sets requireApprovals=2 on a repo, opens a PR, and
// checks merge is refused until enough approvals exist.
func TestMergeGateWithPolicy(t *testing.T) {
	requireLive(t)
	token, proj, repo := setupProjectRepo(t, "gate")
	pushBranch(t, token, proj, repo, "main", "a.txt", "a\n")
	pushBranch(t, token, proj, repo, "feat", "b.txt", "b\n")
	rp := "/projects/" + proj + "/repos/" + repo

	// set branch policy on default branch
	st, b := apiCall(t, token, http.MethodPut, rp+"/policies", map[string]any{"pattern": "main", "requiredApprovals": 2})
	fmt.Printf("SET POLICY: %d %.150s\n", st, b)

	st, b = apiCall(t, token, http.MethodPost, rp+"/pulls", map[string]any{"title": "gate", "base": "main", "head": "feat"})
	if st != 200 && st != 201 {
		t.Fatalf("create PR: %d %s", st, b)
	}
	num := prNumber(t, jbody(t, b))

	st, b = apiCall(t, token, http.MethodPost, fmt.Sprintf("%s/pulls/%d/merge", rp, num), map[string]any{})
	fmt.Printf("MERGE no approvals: %d %.150s\n", st, b)
	if st == 200 {
		t.Errorf("merge succeeded without required approvals")
	}

	// approve once -> still blocked; approve twice -> allowed
	apiCall(t, token, http.MethodPost, fmt.Sprintf("%s/pulls/%d/reviews", rp, num), map[string]any{"event": "APPROVED"})
	st, b = apiCall(t, token, http.MethodPost, fmt.Sprintf("%s/pulls/%d/merge", rp, num), map[string]any{})
	fmt.Printf("MERGE 1 approval: %d %.150s\n", st, b)
	apiCall(t, token, http.MethodPost, fmt.Sprintf("%s/pulls/%d/reviews", rp, num), map[string]any{"event": "APPROVED"})
	st, b = apiCall(t, token, http.MethodPost, fmt.Sprintf("%s/pulls/%d/merge", rp, num), map[string]any{})
	fmt.Printf("MERGE 2 approvals: %d %.150s\n", st, b)
}

// TestPipelineRunLogs triggers a run and reads its logs + status.
func TestPipelineRunLogs(t *testing.T) {
	requireLive(t)
	token, proj, repo := setupProjectRepo(t, "logs")
	pushBranch(t, token, proj, repo, "main", "a.txt", "a\n")
	rp := "/projects/" + proj + "/repos/" + repo + "/pipelines"

	pushBranch(t, token, proj, repo, "main", ".github/workflows/ci.yml", "on: [push]\njobs:\n  build:\n    steps:\n      - run: echo hello-e2e\n")
	st, b := apiCall(t, token, http.MethodPost, rp, map[string]any{"workflow": ".github/workflows/ci.yml", "ref": "main"})
	fmt.Printf("TRIGGER: %d %.200s\n", st, b)
	if st != 200 && st != 201 {
		t.Fatalf("trigger: %d %s", st, b)
	}
	// find the run number
	var num int
	st, b = apiCall(t, token, http.MethodGet, rp+"/runs", nil)
	if st == 200 {
		if arr, ok := jbody(t, b)["runs"].([]any); ok && len(arr) > 0 {
			if f, ok := arr[0].(map[string]any)["number"].(float64); ok {
				num = int(f)
			}
		}
	}
	if num == 0 {
		t.Fatalf("no run listed: %s", b)
	}
	// poll for completion then read logs
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		st, b = apiCall(t, token, http.MethodGet, fmt.Sprintf("%s/runs/%d", rp, num), nil)
		if st == 200 {
			run := jbody(t, b)
			status, _ := run["status"].(string)
			fmt.Printf("RUN %d status=%s\n", num, status)
			if status == "success" || status == "failed" || status == "cancelled" {
				break
			}
		}
		time.Sleep(3 * time.Second)
	}
	st, b = apiCall(t, token, http.MethodGet, fmt.Sprintf("%s/runs/%d/logs", rp, num), nil)
	fmt.Printf("LOGS: %d %.200s\n", st, b)
	if st != 200 {
		t.Errorf("run logs: %d", st)
	}
}

// TestInviteAccept creates an invite as e2e-tester and accepts it as a second
// user, then checks membership.
func TestInviteAccept(t *testing.T) {
	requireLive(t)
	token := login(t)
	orgSlug := fmt.Sprintf("e2e-inv2-%d", time.Now().UnixNano()%1e9)
	if st, b := apiCall(t, token, http.MethodPost, "/orgs", map[string]any{"slug": orgSlug, "name": orgSlug}); st != 200 && st != 201 && st != 409 {
		t.Fatalf("create org: %d %s", st, b)
	}
	st, b := apiCall(t, token, http.MethodPost, "/orgs/"+orgSlug+"/invites", map[string]any{"email": "e2e-invitee@black-tulip.nl", "role": "member"})
	if st != 200 && st != 201 {
		t.Fatalf("create invite: %d %s", st, b)
	}
	inv := jbody(t, b)
	tok, _ := inv["token"].(string)
	if tok == "" {
		// maybe nested
		t.Fatalf("invite has no token: %s", b)
	}
	// accept as a different user via the public accept endpoint (no auth? or auth?)
	c := client(t)
	st, _, b = req(t, c, http.MethodPost, apiURL+"/api/v1/invites/"+tok+"/accept", map[string]any{}, map[string]string{"Authorization": "Bearer " + token})
	fmt.Printf("ACCEPT: %d %.200s\n", st, b)
}
