package platform_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/nielsuitterdijk22/quill/internal/platform"
)

// D2 cutover: engine repos get a native issue tracker in Postgres (Forgejo
// issues 502'd for every migrated/new repo). Numbers share the per-repo space
// with pull requests, like Forgejo/GitHub — "#3" is never ambiguous.
func TestEngineIssuesLifecycle(t *testing.T) {
	browseGitAvailable(t)
	svc, st, gitURL := browseFixture(t)
	ctx := context.Background()
	owner := makeUser(t, st, "issue-owner")
	act := actor(owner)
	if _, err := svc.CreateProject(ctx, act, platform.CreateProjectInput{Slug: "issues-acme", Name: "acme"}); err != nil {
		t.Fatalf("project: %v", err)
	}
	if _, err := svc.CreateRepo(ctx, act, "issues-acme", platform.CreateRepoInput{Slug: "tracker", Name: "tracker"}); err != nil {
		t.Fatalf("repo: %v", err)
	}

	// #1 is an issue.
	is1, err := svc.CreateIssue(ctx, act, "issues-acme", "tracker", "Crash on start", "stack trace here")
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	if is1.Number != 1 || is1.State != "open" || is1.User == nil || is1.User.Login != "issue-owner" {
		t.Fatalf("issue = %+v", is1)
	}

	// #2 is a PR — the shared number space.
	work := t.TempDir()
	if out, err := exec.Command("git", "clone", "-q", gitURL+"/issues-acme/tracker", work).CombinedOutput(); err != nil {
		t.Fatalf("clone: %v\n%s", err, out)
	}
	os.WriteFile(filepath.Join(work, "a.txt"), []byte("a\n"), 0o644)
	browseGit(t, work, "add", "-A")
	browseGit(t, work, "commit", "-qm", "base")
	if out, err := browseGitPush(t, work, "origin", "main"); err != nil {
		t.Fatalf("push main: %v\n%s", err, out)
	}
	browseGit(t, work, "checkout", "-qb", "feature")
	os.WriteFile(filepath.Join(work, "b.txt"), []byte("b\n"), 0o644)
	browseGit(t, work, "add", "-A")
	browseGit(t, work, "commit", "-qm", "feature")
	if out, err := browseGitPush(t, work, "origin", "feature"); err != nil {
		t.Fatalf("push feature: %v\n%s", err, out)
	}
	_, pr, err := svc.CreatePull(ctx, act, "issues-acme", "tracker", platform.CreatePullInput{Title: "fix", Head: "feature", Base: "main"})
	if err != nil {
		t.Fatalf("CreatePull: %v", err)
	}
	if pr.Number != 2 {
		t.Fatalf("PR number = %d, want 2 (shared with issue #1)", pr.Number)
	}

	// #3 is the next issue.
	is3, err := svc.CreateIssue(ctx, act, "issues-acme", "tracker", "Second bug", "")
	if err != nil {
		t.Fatalf("CreateIssue 2: %v", err)
	}
	if is3.Number != 3 {
		t.Fatalf("issue number = %d, want 3", is3.Number)
	}

	// Comment, then close.
	c, err := svc.CreateIssueComment(ctx, act, "issues-acme", "tracker", 1, "repro attached")
	if err != nil {
		t.Fatalf("comment: %v", err)
	}
	if c.Body != "repro attached" || c.ID == 0 || c.User == nil {
		t.Fatalf("comment = %+v", c)
	}
	closed := "closed"
	if _, err := svc.EditIssue(ctx, act, "issues-acme", "tracker", 1, &closed, nil); err != nil {
		t.Fatalf("close: %v", err)
	}

	got, comments, err := svc.GetIssue(ctx, act, "issues-acme", "tracker", 1)
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if got.State != "closed" || got.Comments != 1 || len(comments) != 1 {
		t.Fatalf("issue #1 = %+v comments=%d", got, len(comments))
	}

	open, err := svc.ListIssues(ctx, act, "issues-acme", "tracker", "open", 1)
	if err != nil {
		t.Fatalf("ListIssues: %v", err)
	}
	if len(open) != 1 || open[0].Number != 3 {
		t.Fatalf("open issues = %+v", open)
	}
	all, _ := svc.ListIssues(ctx, act, "issues-acme", "tracker", "all", 1)
	if len(all) != 2 {
		t.Fatalf("all issues = %d, want 2 (PRs are not issues)", len(all))
	}

	// Non-members see nothing (private repo).
	stranger := actor(makeUser(t, st, "issue-stranger"))
	if _, err := svc.ListIssues(ctx, stranger, "issues-acme", "tracker", "all", 1); err == nil {
		t.Fatal("non-member listed private issues")
	}
	if _, err := svc.CreateIssue(ctx, stranger, "issues-acme", "tracker", "spam", ""); err == nil {
		t.Fatal("non-member created an issue")
	}
	if _, _, err := svc.GetIssue(ctx, act, "issues-acme", "tracker", 2); err == nil {
		// #2 is a PR: the issue view must not leak it as an issue.
		t.Fatal("PR #2 served as an issue")
	}
}
