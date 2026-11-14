package platform_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nielsuitterdijk22/quill/internal/platform"
)

// Forgejo removal (2026-11-14): import, fork, delete and storage metering
// work on the own engine alone.
func TestEngineForkImportDeleteAndStorage(t *testing.T) {
	browseGitAvailable(t)
	svc, st, gitURL := browseFixture(t)
	svc = svc.WithImportURLCheck(func(string) error { return nil })
	ctx := context.Background()
	act := actor(makeUser(t, st, "seed-owner"))
	for _, p := range []string{"seed-src", "seed-dst"} {
		if _, err := svc.CreateProject(ctx, act, platform.CreateProjectInput{Slug: p, Name: p}); err != nil {
			t.Fatalf("project %s: %v", p, err)
		}
	}
	src, err := svc.CreateRepo(ctx, act, "seed-src", platform.CreateRepoInput{Slug: "app", Name: "app"})
	if err != nil {
		t.Fatalf("repo: %v", err)
	}
	work := t.TempDir()
	if out, err := exec.Command("git", "clone", "-q", gitURL+"/seed-src/app", work).CombinedOutput(); err != nil {
		t.Fatalf("clone: %v\n%s", err, out)
	}
	if err := os.WriteFile(filepath.Join(work, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	browseGit(t, work, "add", "-A")
	browseGit(t, work, "commit", "-qm", "init")
	head := browseGit(t, work, "rev-parse", "HEAD")
	if out, err := browseGitPush(t, work, "origin", "main"); err != nil {
		t.Fatalf("push: %v\n%s", err, out)
	}

	tipOf := func(project, repo string) string {
		t.Helper()
		_, commits, err := svc.ListCommits(ctx, act, project, repo, "main", "", 1)
		if err != nil || len(commits) == 0 {
			t.Fatalf("commits of %s/%s: %v %d", project, repo, err, len(commits))
		}
		return commits[0].SHA
	}

	// Fork into the second project: same history, its own repo.
	fork, err := svc.ForkRepo(ctx, act, "seed-src", "app", platform.ForkRepoInput{TargetProjectSlug: "seed-dst", Slug: "app-fork"})
	if err != nil {
		t.Fatalf("fork: %v", err)
	}
	if fork.DefaultBranch != "main" || tipOf("seed-dst", "app-fork") != head {
		t.Fatalf("fork: branch %q / history mismatch", fork.DefaultBranch)
	}

	// Import from a URL (the engine's own smart-HTTP stands in for GitHub).
	if _, err := svc.CreateRepo(ctx, act, "seed-dst", platform.CreateRepoInput{
		Slug: "imported", Name: "imported",
		CloneFrom: &platform.CloneSource{URL: gitURL + "/seed-src/app"},
	}); err != nil {
		t.Fatalf("import: %v", err)
	}
	if tipOf("seed-dst", "imported") != head {
		t.Fatal("imported history mismatch")
	}

	// Storage is metered from the engine's referee rows.
	project, err := st.GetProjectByID(ctx, src.ProjectID)
	if err != nil {
		t.Fatal(err)
	}
	if n, err := st.SumEngineStorageByTenant(ctx, project.TenantID); err != nil || n <= 0 {
		t.Fatalf("engine storage = %d, %v", n, err)
	}

	// Delete + re-create the same slug (a leftover manifest used to block it).
	if err := svc.DeleteRepo(ctx, act, "seed-dst", "app-fork"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := svc.CreateRepo(ctx, act, "seed-dst", platform.CreateRepoInput{Slug: "app-fork", Name: "app-fork"}); err != nil {
		t.Fatalf("re-create after delete: %v", err)
	}
}

// Imports refuse non-https and internal addresses (SSRF guard).
func TestImportURLPolicy(t *testing.T) {
	browseGitAvailable(t)
	svc, st, _ := browseFixture(t)
	ctx := context.Background()
	act := actor(makeUser(t, st, "imp-owner"))
	if _, err := svc.CreateProject(ctx, act, platform.CreateProjectInput{Slug: "imp-pol", Name: "imp-pol"}); err != nil {
		t.Fatal(err)
	}
	for _, u := range []string{"file:///etc", "http://example.com/x.git", "https://127.0.0.1/x.git", "https://169.254.169.254/x", "https://user:pw@github.com/a/b"} {
		_, err := svc.CreateRepo(ctx, act, "imp-pol", platform.CreateRepoInput{Slug: "x", Name: "x", CloneFrom: &platform.CloneSource{URL: u}})
		if err == nil || !strings.Contains(err.Error(), "import") && !strings.Contains(err.Error(), "resolves") && !strings.Contains(err.Error(), "not supported") {
			t.Fatalf("%s: want refusal, got %v", u, err)
		}
	}
}
