package pipeline

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Checkout clones the repository into dest so act has the repo files to run
// against (act skips actions/checkout and uses the local workspace instead).
//
// It shallow-clones the branch, then best-effort checks out the exact commit.
// authHeader may contain credentials; errors never include it so tokens can't
// leak into logs or the run record.
func Checkout(ctx context.Context, cloneURL, ref, sha, dest, authHeader string) error {
	args := []string{"clone", "--quiet", "--depth", "50", "--no-tags"}
	if strings.TrimSpace(authHeader) != "" {
		args = append(args, "-c", "http.extraHeader="+authHeader)
	}
	if strings.TrimSpace(ref) != "" {
		args = append(args, "--branch", ref)
	}
	args = append(args, cloneURL, dest)

	if out, err := runGit(ctx, "", args...); err != nil {
		return fmt.Errorf("clone repository: %w%s", err, sanitizeGitOutput(out, cloneURL))
	}

	if strings.TrimSpace(sha) != "" {
		// The exact commit may sit outside the shallow history; failing to land on
		// it is non-fatal — the branch tip is a reasonable fallback.
		_, _ = runGit(ctx, dest, "checkout", "--quiet", sha)
	}
	return nil
}

func runGit(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// sanitizeGitOutput returns git's output with the clone URL (and thus any
// embedded credential) redacted, prefixed with ": " when non-empty.
func sanitizeGitOutput(out, cloneURL string) string {
	out = strings.TrimSpace(out)
	if out == "" {
		return ""
	}
	if cloneURL != "" {
		out = strings.ReplaceAll(out, cloneURL, "<repo>")
	}
	if i := strings.Index(out, "Authorization:"); i >= 0 {
		out = out[:i] + "Authorization: <redacted>"
	}
	return ": " + out
}
