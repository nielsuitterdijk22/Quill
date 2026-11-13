package platform_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/nielsuitterdijk22/quill/internal/config"
	"github.com/nielsuitterdijk22/quill/internal/forgejo"
	"github.com/nielsuitterdijk22/quill/internal/platform"
)

// The cross-repo PR list (/pulls) must not ask Forgejo about repos the engine
// owns: after the D2 migration every repo is engine-backed, and one 404'ing
// Forgejo call per repo (plus serial hydration) pushed the page past 60 s.
func TestListOpenPullsSkipsForgejoForEngineRepos(t *testing.T) {
	browseGitAvailable(t)
	svc, st, gitURL := browseFixture(t)
	var forgejoCalls atomic.Int64
	fj := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		forgejoCalls.Add(1)
		http.NotFound(w, r)
	}))
	t.Cleanup(fj.Close)

	ctx := context.Background()
	act := actor(makeUser(t, st, "pulllist-owner"))
	if _, err := svc.CreateProject(ctx, act, platform.CreateProjectInput{Slug: "pulllist", Name: "pulllist"}); err != nil {
		t.Fatalf("project: %v", err)
	}
	for _, r := range []string{"one", "two", "three"} {
		if _, err := svc.CreateRepo(ctx, act, "pulllist", platform.CreateRepoInput{Slug: r, Name: r}); err != nil {
			t.Fatalf("repo %s: %v", r, err)
		}
	}
	// One real PR on "one".
	work := t.TempDir()
	if out, err := exec.Command("git", "clone", "-q", gitURL+"/pulllist/one", work).CombinedOutput(); err != nil {
		t.Fatalf("clone: %v\n%s", err, out)
	}
	os.WriteFile(filepath.Join(work, "a.txt"), []byte("a\n"), 0o644)
	browseGit(t, work, "add", "-A")
	browseGit(t, work, "commit", "-qm", "base")
	if out, err := browseGitPush(t, work, "origin", "main"); err != nil {
		t.Fatalf("push: %v\n%s", err, out)
	}
	browseGit(t, work, "checkout", "-qb", "f")
	os.WriteFile(filepath.Join(work, "b.txt"), []byte("b\n"), 0o644)
	browseGit(t, work, "add", "-A")
	browseGit(t, work, "commit", "-qm", "f")
	if out, err := browseGitPush(t, work, "origin", "f"); err != nil {
		t.Fatalf("push f: %v\n%s", err, out)
	}
	if _, _, err := svc.CreatePull(ctx, act, "pulllist", "one", platform.CreatePullInput{Title: "t", Head: "f", Base: "main"}); err != nil {
		t.Fatalf("CreatePull: %v", err)
	}

	// Forgejo "on" (as in production during the cutover) for the listing.
	svc = svc.WithForgejo(forgejo.New(config.ForgejoConfig{BaseURL: fj.URL, AdminToken: "t"}))
	pulls, err := svc.ListOpenPulls(ctx, act, platform.ListOpenPullsInput{ProjectSlug: "pulllist"})
	if err != nil {
		t.Fatalf("ListOpenPulls: %v", err)
	}
	if len(pulls) != 1 || pulls[0].RepoSlug != "one" {
		t.Fatalf("pulls = %+v", pulls)
	}
	if n := forgejoCalls.Load(); n != 0 {
		t.Fatalf("Forgejo called %d times for engine-owned repos", n)
	}
}
