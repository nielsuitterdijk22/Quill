package pipeline

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A clone that fails is retried, and the error says whether a credential was
// sent. A clone that works on a later attempt succeeds.
func TestCheckoutRetriesAndExplainsAuth(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	old := cloneBackoff
	cloneBackoff = func(int) time.Duration { return time.Millisecond }
	defer func() { cloneBackoff = old }()

	dest := filepath.Join(t.TempDir(), "w")
	err := Checkout(context.Background(), "file:///nonexistent/repo.git", "refs/heads/main", "", dest, "")
	if err == nil || !strings.Contains(err.Error(), "clone repository") {
		t.Fatalf("expected a clone error, got %v", err)
	}

	// Source repo that appears only after the first attempt fails.
	src := filepath.Join(t.TempDir(), "src")
	run := func(dir string, args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	os.MkdirAll(src, 0o755)
	attempts := 0
	cloneBackoff = func(int) time.Duration {
		attempts++
		run(src, "init", "-q", "-b", "main")
		os.WriteFile(filepath.Join(src, "f"), []byte("x"), 0o644)
		run(src, "add", ".")
		run(src, "commit", "-q", "-m", "c")
		return time.Millisecond
	}
	dest2 := filepath.Join(t.TempDir(), "w2")
	if err := Checkout(context.Background(), "file://"+src, "refs/heads/main", "", dest2, ""); err != nil {
		t.Fatalf("clone should succeed on retry: %v", err)
	}
	if attempts == 0 {
		t.Error("expected at least one retry")
	}
}

func TestCredentialHint(t *testing.T) {
	out := "fatal: could not read Username for 'https://git.example': terminal prompts disabled"
	if h := credentialHint(out, ""); !strings.Contains(h, "no clone credential") {
		t.Errorf("missing-credential hint = %q", h)
	}
	if h := credentialHint(out, "Authorization: Basic x"); !strings.Contains(h, "refused it") {
		t.Errorf("rejected-credential hint = %q", h)
	}
	if h := credentialHint("fatal: repository not found", ""); h != "" {
		t.Errorf("unrelated failure got a hint: %q", h)
	}
}
