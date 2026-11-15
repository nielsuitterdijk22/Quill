package platform_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nielsuitterdijk22/quill/internal/gitengine"
	"github.com/nielsuitterdijk22/quill/internal/platform"
)

// A merge's commit must reach the bucket, not just the merging instance's
// cache (2026-11-15: merges wrote the merge commit as loose objects and the
// bucket sync uploaded only packs, so the referee pointed main at a commit
// no other instance could load — tempo's contents page and clones broke).
// Another instance is simulated by hydrating into a fresh cache.
func TestEngineMergeCommitReachesBucket(t *testing.T) {
	browseGitAvailable(t)
	svc, st, gitURL, mem := browseFixtureMem(t)
	ctx := context.Background()
	const slug = "mergebucket"
	act := actor(makeUser(t, st, slug+"-owner"))
	project, err := svc.CreateProject(ctx, act, platform.CreateProjectInput{Slug: slug, Name: slug})
	if err != nil {
		t.Fatalf("project: %v", err)
	}
	if _, err := svc.CreateRepo(ctx, act, slug, platform.CreateRepoInput{Slug: "app", Name: "app"}); err != nil {
		t.Fatalf("repo: %v", err)
	}
	work := t.TempDir()
	if out, err := exec.Command("git", "clone", "-q", gitURL+"/"+slug+"/app", work).CombinedOutput(); err != nil {
		t.Fatalf("clone: %v\n%s", err, out)
	}
	write := func(f string) {
		if err := os.WriteFile(filepath.Join(work, f), []byte(f+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		browseGit(t, work, "add", "-A")
		browseGit(t, work, "commit", "-qm", f)
	}
	write("base.txt")
	if out, err := browseGitPush(t, work, "origin", "main"); err != nil {
		t.Fatalf("push main: %v\n%s", err, out)
	}
	browseGit(t, work, "checkout", "-q", "-b", "feature")
	write("feature.txt")
	if out, err := browseGitPush(t, work, "origin", "feature"); err != nil {
		t.Fatalf("push feature: %v\n%s", err, out)
	}
	// Move main on so the merge needs a real merge commit.
	browseGit(t, work, "checkout", "-q", "main")
	write("main2.txt")
	if out, err := browseGitPush(t, work, "origin", "main"); err != nil {
		t.Fatalf("push main2: %v\n%s", err, out)
	}
	if _, _, err := svc.CreatePull(ctx, act, slug, "app", platform.CreatePullInput{Title: "f", Head: "feature", Base: "main"}); err != nil {
		t.Fatalf("CreatePull: %v", err)
	}
	_, merged, err := svc.MergePull(ctx, act, slug, "app", 1, "merge")
	if err != nil || merged.MergeCommit == "" {
		t.Fatalf("MergePull: %+v %v", merged, err)
	}

	eng := gitengine.New(mem)
	fresh := &gitengine.RepoCache{Root: t.TempDir(), Engine: eng, Packs: gitengine.NewBucketPackSource(mem)}
	dir, err := fresh.Hydrate(ctx, project.TenantID.String(), slug, "app")
	if err != nil {
		t.Fatalf("fresh hydrate: %v", err)
	}
	out, err := exec.Command("git", "-C", dir, "cat-file", "-t", merged.MergeCommit).CombinedOutput()
	if err != nil || strings.TrimSpace(string(out)) != "commit" {
		t.Fatalf("merge commit %s missing from the bucket: %s %v", merged.MergeCommit, out, err)
	}
	if out, err := exec.Command("git", "-C", dir, "ls-tree", "--name-only", "main").CombinedOutput(); err != nil || !strings.Contains(string(out), "feature.txt") {
		t.Fatalf("main tree from a fresh cache: %s %v", out, err)
	}
}
