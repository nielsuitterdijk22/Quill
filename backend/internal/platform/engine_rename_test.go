package platform_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/nielsuitterdijk22/quill/internal/platform"
)

// TestEngineRenamesMoveStorage: renaming a username (which renames the
// personal project) or a repo slug moves the engine repo's bucket objects, so
// the code stays readable under the new names (2026-11-14 incident: a
// username change left every repo of the personal project unreadable).
func TestEngineRenamesMoveStorage(t *testing.T) {
	browseGitAvailable(t)
	svc, st, gitURL := browseFixture(t)
	ctx := context.Background()
	uid := makeUser(t, st, "ren.old")
	act := actor(uid)
	if err := svc.CreatePersonalProject(ctx, uid, "ren.old"); err != nil {
		t.Fatalf("personal project: %v", err)
	}
	if _, err := svc.CreateRepo(ctx, act, "ren.old", platform.CreateRepoInput{Slug: "app", Name: "app"}); err != nil {
		t.Fatalf("repo: %v", err)
	}
	work := t.TempDir()
	if out, err := exec.Command("git", "clone", "-q", gitURL+"/ren.old/app", work).CombinedOutput(); err != nil {
		t.Fatalf("clone: %v\n%s", err, out)
	}
	os.WriteFile(filepath.Join(work, "README.md"), []byte("hello\n"), 0o644)
	browseGit(t, work, "add", "-A")
	browseGit(t, work, "commit", "-qm", "init")
	head := browseGit(t, work, "rev-parse", "HEAD")
	if out, err := browseGitPush(t, work, "origin", "main"); err != nil {
		t.Fatalf("push: %v\n%s", err, out)
	}
	tip := func(project, repo string) string {
		t.Helper()
		_, commits, err := svc.ListCommits(ctx, act, project, repo, "main", "", 1)
		if err != nil || len(commits) == 0 {
			t.Fatalf("commits of %s/%s: %v (%d)", project, repo, err, len(commits))
		}
		return commits[0].SHA
	}
	if tip("ren.old", "app") != head {
		t.Fatal("history before rename")
	}

	if _, err := svc.SetUsername(ctx, uid, "rennew"); err != nil {
		t.Fatalf("set username: %v", err)
	}
	if got := tip("rennew", "app"); got != head {
		t.Fatalf("after username change: tip %s, want %s", got, head)
	}

	slug := "app2"
	if _, err := svc.UpdateRepo(ctx, act, "rennew", "app", platform.UpdateRepoInput{Slug: &slug}); err != nil {
		t.Fatalf("rename repo: %v", err)
	}
	if got := tip("rennew", "app2"); got != head {
		t.Fatalf("after repo rename: tip %s, want %s", got, head)
	}
	// And it still takes pushes under the new name.
	browseGit(t, work, "remote", "set-url", "origin", gitURL+"/rennew/app2")
	os.WriteFile(filepath.Join(work, "b.txt"), []byte("b\n"), 0o644)
	browseGit(t, work, "add", "-A")
	browseGit(t, work, "commit", "-qm", "second")
	second := browseGit(t, work, "rev-parse", "HEAD")
	if out, err := browseGitPush(t, work, "origin", "main"); err != nil {
		t.Fatalf("push after rename: %v\n%s", err, out)
	}
	if got := tip("rennew", "app2"); got != second {
		t.Fatalf("after push: tip %s, want %s", got, second)
	}
}
