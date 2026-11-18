package platform

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// TestBareListBranchesAheadAndUpdated checks the data behind the "recently
// pushed branch → open a pull request" shortcut: how many commits a branch has
// beyond the default branch, and when its tip last changed (committer date, so
// a rebase counts as an update).
func TestBareListBranchesAheadAndUpdated(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	run := func(env []string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
		cmd.Env = append(cmd.Env, env...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	commit := func(env []string, file string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, file), []byte(file), 0o644); err != nil {
			t.Fatal(err)
		}
		run(nil, "add", "-A")
		run(env, "commit", "-qm", "add "+file)
	}

	run(nil, "init", "-q", "-b", "main")
	commit(nil, "base.txt")

	run(nil, "checkout", "-q", "-b", "same-as-main") // nothing new
	run(nil, "checkout", "-q", "-b", "feature", "main")
	commit(nil, "a.txt")
	commit(nil, "b.txt")

	// An old branch: authored and committed two weeks ago.
	old := time.Now().Add(-14 * 24 * time.Hour).Format(time.RFC3339)
	run(nil, "checkout", "-q", "-b", "stale", "main")
	commit([]string{"GIT_AUTHOR_DATE=" + old, "GIT_COMMITTER_DATE=" + old}, "c.txt")

	// A rebased branch keeps an old author date but a fresh committer date.
	run(nil, "checkout", "-q", "-b", "rebased", "main")
	commit([]string{"GIT_AUTHOR_DATE=" + old}, "d.txt")
	run(nil, "checkout", "-q", "main")

	branches, err := bareListBranches(dir, "main")
	if err != nil {
		t.Fatalf("bareListBranches: %v", err)
	}
	by := map[string]int{}
	for i, b := range branches {
		by[b.Name] = i
	}
	get := func(name string) (ahead int, fresh bool) {
		t.Helper()
		i, ok := by[name]
		if !ok {
			t.Fatalf("branch %q missing from %+v", name, branches)
		}
		b := branches[i]
		return b.Ahead, time.Since(b.UpdatedAt) < time.Hour
	}

	if a, fresh := get("feature"); a != 2 || !fresh {
		t.Errorf("feature: ahead=%d fresh=%v, want 2/true", a, fresh)
	}
	if a, _ := get("same-as-main"); a != 0 {
		t.Errorf("same-as-main: ahead=%d, want 0", a)
	}
	if a, fresh := get("main"); a != 0 || !fresh {
		t.Errorf("main: ahead=%d fresh=%v, want 0/true (the default branch is never ahead of itself)", a, fresh)
	}
	// Outside aheadWindow the count is skipped (bounds the cost on repos with
	// many old branches): 0 means "not computed", and it is not fresh either.
	if a, fresh := get("stale"); a != 0 || fresh {
		t.Errorf("stale: ahead=%d fresh=%v, want 0/false (outside the ahead window)", a, fresh)
	}
	if a, fresh := get("rebased"); a != 1 || !fresh {
		t.Errorf("rebased: ahead=%d fresh=%v, want 1/true (committer date, not author date)", a, fresh)
	}

	// A default branch that does not exist (empty repo, renamed default) must
	// not fail the listing; ahead is simply unknown (0).
	if _, err := bareListBranches(dir, "nope"); err != nil {
		t.Fatalf("missing default branch must not error: %v", err)
	}
}
