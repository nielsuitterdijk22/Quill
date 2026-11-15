package platform_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/nielsuitterdijk22/quill/internal/platform"
)

// A PR follows its branch (2026-11-15): commits pushed after it was opened
// appear in its diff, commits and file count; once merged it keeps showing
// what it changed (it used to say "0 files changed").
func TestPullFollowsBranchAndKeepsStatsAfterMerge(t *testing.T) {
	browseGitAvailable(t)
	svc, st, gitURL := browseFixture(t)
	ctx := context.Background()
	act := actor(makeUser(t, st, "heads-owner"))
	if _, err := svc.CreateProject(ctx, act, platform.CreateProjectInput{Slug: "heads-acme", Name: "acme"}); err != nil {
		t.Fatalf("project: %v", err)
	}
	if _, err := svc.CreateRepo(ctx, act, "heads-acme", platform.CreateRepoInput{Slug: "app", Name: "app"}); err != nil {
		t.Fatalf("repo: %v", err)
	}
	work := t.TempDir()
	if out, err := exec.Command("git", "clone", "-q", gitURL+"/heads-acme/app", work).CombinedOutput(); err != nil {
		t.Fatalf("clone: %v\n%s", err, out)
	}
	commit := func(file, msg string) {
		os.WriteFile(filepath.Join(work, file), []byte(file+"\n"), 0o644)
		browseGit(t, work, "add", "-A")
		browseGit(t, work, "commit", "-qm", msg)
	}
	push := func(branch string) {
		if out, err := browseGitPush(t, work, "origin", branch); err != nil {
			t.Fatalf("push %s: %v\n%s", branch, err, out)
		}
	}
	commit("a.txt", "base")
	push("main")
	browseGit(t, work, "checkout", "-qb", "feature")
	commit("b.txt", "one")
	push("feature")
	_, pr, err := svc.CreatePull(ctx, act, "heads-acme", "app", platform.CreatePullInput{Title: "f", Head: "feature", Base: "main"})
	if err != nil {
		t.Fatalf("CreatePull: %v", err)
	}
	n := int(pr.Number)

	commit("c.txt", "two")
	push("feature")
	_, got, err := svc.GetPull(ctx, act, "heads-acme", "app", n)
	if err != nil || got.ChangedFiles != 2 {
		t.Fatalf("after a second push: files=%d err=%v", got.ChangedFiles, err)
	}
	_, files, err := svc.GetPullDiff(ctx, act, "heads-acme", "app", n)
	if err != nil || len(files) != 2 {
		t.Fatalf("diff files = %d (%v)", len(files), err)
	}
	_, commits, err := svc.ListPullCommits(ctx, act, "heads-acme", "app", n)
	if err != nil || len(commits) != 2 {
		t.Fatalf("commits = %d (%v)", len(commits), err)
	}

	if _, _, err := svc.MergePull(ctx, act, "heads-acme", "app", n, "merge"); err != nil {
		t.Fatalf("merge: %v", err)
	}
	_, got, err = svc.GetPull(ctx, act, "heads-acme", "app", n)
	if err != nil || !got.Merged || got.ChangedFiles != 2 || got.Additions != 2 {
		t.Fatalf("merged pull: merged=%v files=%d adds=%d err=%v", got.Merged, got.ChangedFiles, got.Additions, err)
	}
	if _, files, err = svc.GetPullDiff(ctx, act, "heads-acme", "app", n); err != nil || len(files) != 2 {
		t.Fatalf("merged diff files = %d (%v)", len(files), err)
	}
}
