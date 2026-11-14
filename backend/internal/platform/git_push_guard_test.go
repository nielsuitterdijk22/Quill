package platform

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/nielsuitterdijk22/quill/internal/gitengine"
)

// These integration tests cover the B3 pre-receive guard (branch policies →
// git push enforcement), gated on both QUILL_TEST_DATABASE_URL and a git
// binary: the guard shells out to git for fast-forward ancestry checks in the
// hydrated repository directory.

// testGitDir builds a bare repo with two commits: base (an ancestor) and
// child (a descendant of base), returning the dir plus both shas.
func testGitDir(t *testing.T) (dir, base, child string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not in PATH; skipping git-backed guard test")
	}
	dir = t.TempDir()
	run := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=quill-test", "GIT_AUTHOR_EMAIL=quill-test@example.com",
			"GIT_COMMITTER_NAME=quill-test", "GIT_COMMITTER_EMAIL=quill-test@example.com")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	run("init", "-q", "--bare", dir)
	// Commits need a work tree: use a scratch clone and push into the bare dir.
	work := t.TempDir()
	runW := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = work
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=quill-test", "GIT_AUTHOR_EMAIL=quill-test@example.com",
			"GIT_COMMITTER_NAME=quill-test", "GIT_COMMITTER_EMAIL=quill-test@example.com")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	runW("init", "-q", "-b", "main", work)
	os.WriteFile(work+"/a.txt", []byte("a\n"), 0o644)
	runW("add", "-A")
	runW("commit", "-qm", "base")
	base = runW("rev-parse", "HEAD")
	os.WriteFile(work+"/b.txt", []byte("b\n"), 0o644)
	runW("add", "-A")
	runW("commit", "-qm", "child")
	child = runW("rev-parse", "HEAD")
	// Import both commits into the bare repo via a pack.
	pack := t.TempDir() + "/probe.pack"
	runW("bundle", "create", pack, "--all")
	run("fetch", "-q", work, "refs/heads/main:refs/heads/main")
	return dir, base, child
}

// TestGitPushGuardEnforcesBlockForcePush verifies the pre-receive guard
// wiring: a BlockForcePush policy on a matched pattern rejects force pushes
// and ref deletes, while fast-forward updates and unmatched refs pass.
func TestGitPushGuardEnforcesBlockForcePush(t *testing.T) {
	svc, st := scopeTestService(t)
	ctx := context.Background()
	owner, repo := seedScopeRepo(t, svc, st, "guard-acme", "guarded")
	if _, err := svc.SetProjectBranchPolicy(ctx, owner, "guard-acme", BranchPolicyInput{
		Pattern: "main", BlockForcePush: true,
	}); err != nil {
		t.Fatalf("set policy: %v", err)
	}

	dir, base, child := testGitDir(t)
	guard := svc.GitPushGuard()
	rcx := gitengine.ReceiveContext{
		ID: repo.ID.String(), Dir: dir,
		TenantID: "test", ProjectSlug: "guard-acme", RepoSlug: "guarded",
	}

	// Fast-forward update on the protected branch passes.
	err := guard.PreReceive(ctx, rcx, []gitengine.PushCommand{{
		Old: base, New: child, Ref: "refs/heads/main",
	}}, "alice")
	if err != nil {
		t.Fatalf("fast-forward update must pass: %v", err)
	}

	// Create (the first push to a new ref) passes — old == zero is not force.
	err = guard.PreReceive(ctx, rcx, []gitengine.PushCommand{{
		Old: fmt.Sprintf("%040s", "0"), New: child, Ref: "refs/heads/main",
	}}, "alice")
	if err != nil {
		t.Fatalf("initial push must pass: %v", err)
	}

	// Force update (rewriting main backwards) is rejected.
	err = guard.PreReceive(ctx, rcx, []gitengine.PushCommand{{
		Old: child, New: base, Ref: "refs/heads/main",
	}}, "alice")
	if err == nil {
		t.Fatal("force push on protected branch must be rejected")
	}

	// Delete on the protected branch is rejected.
	err = guard.PreReceive(ctx, rcx, []gitengine.PushCommand{{
		Old: child, New: fmt.Sprintf("%040s", "0"), Ref: "refs/heads/main",
	}}, "alice")
	if err == nil {
		t.Fatal("delete of protected branch must be rejected")
	}

	// A ref with no matching policy may be force-pushed.
	err = guard.PreReceive(ctx, rcx, []gitengine.PushCommand{{
		Old: child, New: base, Ref: "refs/heads/feature/experiment",
	}}, "alice")
	if err != nil {
		t.Fatalf("unprotected ref force push must pass: %v", err)
	}

	// Multi-command push: one protected force update poisons the whole push.
	err = guard.PreReceive(ctx, rcx, []gitengine.PushCommand{
		{Old: base, New: child, Ref: "refs/heads/feature/ok"},
		{Old: child, New: base, Ref: "refs/heads/main"},
	}, "alice")
	if err == nil {
		t.Fatal("mixed push containing a protected force update must be rejected")
	}
}

// TestGitPushGuardWithoutPolicy allows everything (policy-free repos must
// keep working — this is the default state of every repo).
func TestGitPushGuardWithoutPolicy(t *testing.T) {
	svc, st := scopeTestService(t)
	ctx := context.Background()
	_, repo := seedScopeRepo(t, svc, st, "guard-free", "free")
	dir, base, child := testGitDir(t)
	guard := svc.GitPushGuard()
	rcx := gitengine.ReceiveContext{ID: repo.ID.String(), Dir: dir}
	err := guard.PreReceive(ctx, rcx, []gitengine.PushCommand{{
		Old: child, New: base, Ref: "refs/heads/main",
	}}, "alice")
	if err != nil {
		t.Fatalf("no policy = no restriction, got: %v", err)
	}
}

// TestGitPushGuardRequirePullRequest: a branch whose policy requires a PR
// refuses direct pushes (even fast-forwards) and deletes; creating it in an
// empty repo and pushing other branches stay allowed.
func TestGitPushGuardRequirePullRequest(t *testing.T) {
	svc, st := scopeTestService(t)
	ctx := context.Background()
	owner, repo := seedScopeRepo(t, svc, st, "guard-pr", "guarded")
	if _, err := svc.SetProjectBranchPolicy(ctx, owner, "guard-pr", BranchPolicyInput{
		Pattern: "main", RequirePullRequest: true,
	}); err != nil {
		t.Fatalf("set policy: %v", err)
	}
	dir, base, child := testGitDir(t)
	guard := svc.GitPushGuard()
	rcx := gitengine.ReceiveContext{ID: repo.ID.String(), Dir: dir, TenantID: "test", ProjectSlug: "guard-pr", RepoSlug: "guarded"}
	zero := fmt.Sprintf("%040s", "0")

	if err := guard.PreReceive(ctx, rcx, []gitengine.PushCommand{{Old: base, New: child, Ref: "refs/heads/main"}}, "alice"); err == nil ||
		!strings.Contains(err.Error(), "pull request") {
		t.Fatalf("direct fast-forward push to a PR-required branch must be refused, got %v", err)
	}
	if err := guard.PreReceive(ctx, rcx, []gitengine.PushCommand{{Old: child, New: zero, Ref: "refs/heads/main"}}, "alice"); err == nil {
		t.Fatal("delete of a PR-required branch must be refused")
	}
	if err := guard.PreReceive(ctx, rcx, []gitengine.PushCommand{{Old: zero, New: child, Ref: "refs/heads/main"}}, "alice"); err != nil {
		t.Fatalf("bootstrapping the branch must pass: %v", err)
	}
	if err := guard.PreReceive(ctx, rcx, []gitengine.PushCommand{{Old: base, New: child, Ref: "refs/heads/feature/x"}}, "alice"); err != nil {
		t.Fatalf("other branches must pass: %v", err)
	}
}
