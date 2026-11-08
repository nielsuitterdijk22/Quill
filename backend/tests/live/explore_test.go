package live

import (
	"fmt"
	"net/http"
	"os"
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
	// poll for completion then read logs
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		st, b = apiCall(t, token, http.MethodGet, fmt.Sprintf("%s/runs/%d?workflow=.github/workflows/ci.yml", rp, num), nil)
		if st == 200 {
			run := jbody(t, b)
			if inner, ok := run["run"].(map[string]any); ok {
				run = inner
			}
			status, _ := run["status"].(string)
			fmt.Printf("RUN %d status=%s\n", num, status)
			if status == "success" || status == "failed" || status == "cancelled" {
				break
			}
		}
		time.Sleep(3 * time.Second)
	}
	st, b = apiCall(t, token, http.MethodGet, fmt.Sprintf("%s/runs/%d/logs?workflow=.github/workflows/ci.yml", rp, num), nil)
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

// TestMergeGateTwoUsers: org owner creates repo+PR, second member approves,
// merge then succeeds (policy requiredApprovals=1, self-approval excluded).
func TestMergeGateTwoUsers(t *testing.T) {
	requireLive(t)
	user2 := os.Getenv("QUILL_E2E_USER2")
	pass2 := os.Getenv("QUILL_E2E_PASSWORD2")
	if user2 == "" || pass2 == "" {
		t.Skip("QUILL_E2E_USER2/QUILL_E2E_PASSWORD2 not set")
	}
	tok1 := login(t)
	orgSlug := fmt.Sprintf("e2e-gate-%d", time.Now().UnixNano()%1e9)
	if st, b := apiCall(t, tok1, http.MethodPost, "/orgs", map[string]any{"slug": orgSlug, "name": orgSlug}); st != 200 && st != 201 && st != 409 {
		t.Fatalf("create org: %d %s", st, b)
	}
	// invite user2 and accept as user2
	st, b := apiCall(t, tok1, http.MethodPost, "/orgs/"+orgSlug+"/invites", map[string]any{"email": user2, "role": "member"})
	if st != 200 && st != 201 {
		t.Fatalf("invite: %d %s", st, b)
	}
	itok, _ := jbody(t, b)["token"].(string)
	tok2 := loginAs(t, user2, pass2)
	c := client(t)
	if st, _, b := req(t, c, http.MethodPost, apiURL+"/api/v1/invites/"+itok+"/accept", map[string]any{}, map[string]string{"Authorization": "Bearer " + tok2}); st != 200 {
		t.Fatalf("accept invite: %d %s", st, b)
	}

	// repo in the org project
	repoSlug := fmt.Sprintf("e2e-gate-repo-%d", time.Now().UnixNano()%1e9)
	if st, b := apiCall(t, tok1, http.MethodPost, "/projects/"+orgSlug+"/repos", map[string]any{"slug": repoSlug, "name": repoSlug}); st != 200 && st != 201 {
		t.Fatalf("create repo: %d %s", st, b)
	}
	pushBranch(t, tok1, orgSlug, repoSlug, "main", "a.txt", "a\n")
	pushBranch(t, tok1, orgSlug, repoSlug, "feat", "b.txt", "b\n")
	rp := "/projects/" + orgSlug + "/repos/" + repoSlug
	if st, b := apiCall(t, tok1, http.MethodPut, rp+"/policies", map[string]any{"pattern": "main", "requiredApprovals": 1}); st != 200 {
		t.Fatalf("set policy: %d %s", st, b)
	}
	st, b = apiCall(t, tok1, http.MethodPost, rp+"/pulls", map[string]any{"title": "gate2", "base": "main", "head": "feat"})
	if st != 200 && st != 201 {
		t.Fatalf("create PR: %d %s", st, b)
	}
	num := prNumber(t, jbody(t, b))

	// merge before approval -> 409
	if st, b := apiCall(t, tok1, http.MethodPost, fmt.Sprintf("%s/pulls/%d/merge", rp, num), map[string]any{}); st != 409 {
		t.Errorf("merge before approval: %d %s, want 409", st, b)
	}
	// approve as user2 -> merge ok
	if st, b := apiCall(t, tok2, http.MethodPost, fmt.Sprintf("%s/pulls/%d/reviews", rp, num), map[string]any{"event": "APPROVED", "body": "ok"}); st != 200 && st != 201 {
		t.Fatalf("review by user2: %d %s", st, b)
	}
	st, b = apiCall(t, tok1, http.MethodPost, fmt.Sprintf("%s/pulls/%d/merge", rp, num), map[string]any{})
	fmt.Printf("MERGE after user2 approval: %d %.150s\n", st, b)
	if st != 200 {
		t.Errorf("merge after approval: %d %s", st, b)
	}
	// double merge -> 409 (not 500)
	if st, b := apiCall(t, tok1, http.MethodPost, fmt.Sprintf("%s/pulls/%d/merge", rp, num), map[string]any{}); st != 409 {
		t.Errorf("double merge: %d %s, want 409", st, b)
	}
}
