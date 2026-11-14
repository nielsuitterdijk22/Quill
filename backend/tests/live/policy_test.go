package live

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// TestBranchPoliciesEnforced: a repo branch policy on main (require PR, block
// force push, 1 approval) on the own engine refuses direct pushes, force
// pushes and deletes; a PR cannot merge without the approval; feature
// branches stay free. Then with approvals relaxed to 0 the PR merges.
func TestBranchPoliciesEnforced(t *testing.T) {
	requireLive(t)
	token, proj, repo := setupProjectRepo(t, "policy")
	st, b := apiCall(t, token, http.MethodPost, "/me/git-token", map[string]any{"name": "e2e"})
	if st != 200 && st != 201 {
		t.Fatalf("git token: %d %s", st, b)
	}
	cred := jbody(t, b)
	tok, _ := cred["token"].(string)
	username, _ := cred["username"].(string)
	dir := t.TempDir()
	sh(t, dir, "git", "clone", gitCloneURL(t, username, tok, proj, repo), "r")
	rd := dir + "/r"
	sh(t, rd, "git", "config", "user.email", "e2e@black-tulip.nl")
	sh(t, rd, "git", "config", "user.name", "e2e")
	write := func(name, body string) {
		if err := os.WriteFile(rd+"/"+name, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("a.txt", "a\n")
	sh(t, rd, "git", "add", ".")
	sh(t, rd, "git", "commit", "-m", "c0")
	write("a.txt", "a2\n")
	sh(t, rd, "git", "add", ".")
	sh(t, rd, "git", "commit", "-m", "c1")
	sh(t, rd, "git", "branch", "-M", "main")
	sh(t, rd, "git", "push", "-u", "origin", "main") // bootstrap: allowed

	rp := "/projects/" + proj + "/repos/" + repo
	setPolicy := func(approvals int) {
		t.Helper()
		st, b := apiCall(t, token, http.MethodPut, rp+"/policies", map[string]any{
			"pattern": "main", "requirePullRequest": true, "blockForcePush": true, "requiredApprovals": approvals,
		})
		if st != 200 && st != 201 {
			t.Fatalf("set policy: %d %s", st, b)
		}
	}
	setPolicy(1)

	gitTry := func(args ...string) (string, error) {
		cmd := exec.Command("git", args...)
		cmd.Dir = rd
		cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
		out, err := cmd.CombinedOutput()
		return redactTokens(string(out)), err
	}

	// Direct push to main: refused with the policy message.
	write("b.txt", "b\n")
	sh(t, rd, "git", "add", ".")
	sh(t, rd, "git", "commit", "-m", "direct")
	out, err := gitTry("push", "origin", "main")
	if err == nil || !strings.Contains(out, "pull request") {
		t.Fatalf("direct push to protected main must be refused with a PR hint; err=%v out=%s", err, out)
	}
	t.Logf("direct push refused: %s", firstLine(out, "pull request"))

	// Force push (rewind main to c0) and delete: refused.
	sh(t, rd, "git", "reset", "--hard", "HEAD~1") // back to c1 (= origin/main)
	if out, err := gitTry("push", "--force", "origin", "HEAD~1:main"); err == nil {
		t.Fatalf("force push must be refused: %s", out)
	}
	if out, err := gitTry("push", "origin", "--delete", "main"); err == nil {
		t.Fatalf("delete of protected main must be refused: %s", out)
	}

	// Feature branch: free. PR without approval: merge blocked.
	sh(t, rd, "git", "checkout", "-b", "feat")
	write("c.txt", "c\n")
	sh(t, rd, "git", "add", ".")
	sh(t, rd, "git", "commit", "-m", "feature")
	sh(t, rd, "git", "push", "-u", "origin", "feat")
	sh(t, rd, "git", "push", "--force", "origin", "feat") // unprotected: fine
	st, b = apiCall(t, token, http.MethodPost, rp+"/pulls", map[string]any{"title": "policy pr", "base": "main", "head": "feat"})
	if st != 200 && st != 201 {
		t.Fatalf("create PR: %d %s", st, b)
	}
	num := prNumber(t, jbody(t, b))
	mst, mb := apiCall(t, token, http.MethodPost, fmt.Sprintf("%s/pulls/%d/merge", rp, num), map[string]any{})
	if mst < 400 || mst >= 500 {
		t.Fatalf("merge without the required approval must be refused (4xx), got %d %s", mst, mb)
	}
	t.Logf("merge refused without approval: %d %.160s", mst, mb)

	// Relax to 0 approvals: the PR merges (PR path is the only way in).
	setPolicy(0)
	mst, mb = apiCall(t, token, http.MethodPost, fmt.Sprintf("%s/pulls/%d/merge", rp, num), map[string]any{})
	if mst != 200 && mst != 204 {
		t.Fatalf("merge after relaxing approvals: %d %s", mst, mb)
	}
	sh(t, rd, "git", "fetch", "origin")
	if got := strings.TrimSpace(sh(t, rd, "git", "ls-tree", "--name-only", "origin/main")); !strings.Contains(got, "c.txt") {
		t.Fatalf("merged main lacks the PR's change: %q", got)
	}
}

func firstLine(out, needle string) string {
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, needle) {
			return strings.TrimSpace(l)
		}
	}
	return ""
}
