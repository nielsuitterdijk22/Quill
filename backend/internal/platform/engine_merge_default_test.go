package platform_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/nielsuitterdijk22/quill/internal/platform"
)

// An empty merge method means "merge" (GitHub API semantics, and what the
// Forgejo path always did) — the engine path answered 400 instead (found by
// the live merge test once it asserted the merge actually happened).
func TestEngineMergeDefaultsToMergeCommit(t *testing.T) {
	browseGitAvailable(t)
	svc, st, gitURL := browseFixture(t)
	ctx := context.Background()
	act := actor(makeUser(t, st, "mergedef-owner"))
	if _, err := svc.CreateProject(ctx, act, platform.CreateProjectInput{Slug: "mergedef", Name: "mergedef"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateRepo(ctx, act, "mergedef", platform.CreateRepoInput{Slug: "r", Name: "r"}); err != nil {
		t.Fatal(err)
	}
	work := t.TempDir()
	if out, err := exec.Command("git", "clone", "-q", gitURL+"/mergedef/r", work).CombinedOutput(); err != nil {
		t.Fatalf("clone: %v\n%s", err, out)
	}
	os.WriteFile(filepath.Join(work, "a.txt"), []byte("a\n"), 0o644)
	browseGit(t, work, "add", "-A")
	browseGit(t, work, "commit", "-qm", "a")
	if out, err := browseGitPush(t, work, "origin", "main"); err != nil {
		t.Fatalf("push: %v\n%s", err, out)
	}
	browseGit(t, work, "checkout", "-qb", "f")
	os.WriteFile(filepath.Join(work, "b.txt"), []byte("b\n"), 0o644)
	browseGit(t, work, "add", "-A")
	browseGit(t, work, "commit", "-qm", "b")
	if out, err := browseGitPush(t, work, "origin", "f"); err != nil {
		t.Fatalf("push f: %v\n%s", err, out)
	}
	if _, _, err := svc.CreatePull(ctx, act, "mergedef", "r", platform.CreatePullInput{Title: "t", Head: "f", Base: "main"}); err != nil {
		t.Fatal(err)
	}
	_, pr, err := svc.MergePull(ctx, act, "mergedef", "r", 1, "")
	if err != nil {
		t.Fatalf("merge with default method: %v", err)
	}
	if !pr.Merged {
		t.Fatalf("PR not merged: %+v", pr)
	}
}
