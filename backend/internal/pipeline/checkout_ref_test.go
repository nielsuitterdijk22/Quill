package pipeline

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Push-triggered runs carry the FULL ref (refs/heads/main, refs/tags/v1) from
// the engine's post-receive, but `git clone --branch` only takes short names:
// every push-triggered run failed with "Remote branch refs/heads/main not
// found in upstream origin" (live showcase, 2026-11-13).
func TestCheckoutAcceptsFullRefs(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not in PATH")
	}
	root := t.TempDir()
	src := filepath.Join(root, "src")
	run := func(dir string, args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@e", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@e")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	os.MkdirAll(src, 0o755)
	run(src, "init", "-q", "-b", "main")
	os.WriteFile(filepath.Join(src, "a.txt"), []byte("a\n"), 0o644)
	run(src, "add", ".")
	run(src, "commit", "-qm", "a")
	run(src, "tag", "v1")
	sha := run(src, "rev-parse", "HEAD")

	for i, ref := range []string{"main", "refs/heads/main", "refs/tags/v1", "v1"} {
		dest := filepath.Join(root, "dest", string(rune('a'+i)))
		if err := Checkout(context.Background(), "file://"+src, ref, sha, dest, ""); err != nil {
			t.Errorf("Checkout(%q): %v", ref, err)
			continue
		}
		if got := run(dest, "rev-parse", "HEAD"); got != sha {
			t.Errorf("Checkout(%q) HEAD = %s, want %s", ref, got, sha)
		}
	}
}
