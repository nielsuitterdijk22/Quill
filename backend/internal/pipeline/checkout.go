package pipeline

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// cloneAttempts bounds retries of a failed clone: the git endpoint sits behind
// an edge proxy and a freshly booted worker, and a single refused request
// (seen as "could not read Username … terminal prompts disabled") should not
// fail a whole run. Backoff grows 2s, 4s.
const cloneAttempts = 3

// cloneBackoff is a var so tests do not sleep.
var cloneBackoff = func(attempt int) time.Duration { return time.Duration(attempt) * 2 * time.Second }

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
	if b := cloneBranchName(ref); b != "" {
		args = append(args, "--branch", b)
	}
	args = append(args, cloneURL, dest)

	var out string
	var err error
	for attempt := 1; attempt <= cloneAttempts; attempt++ {
		if out, err = runGit(ctx, "", args...); err == nil {
			break
		}
		// A failed clone can leave a partial directory that blocks the retry.
		_ = os.RemoveAll(dest)
		if attempt == cloneAttempts || ctx.Err() != nil {
			break
		}
		select {
		case <-ctx.Done():
		case <-time.After(cloneBackoff(attempt)):
		}
	}
	if err != nil {
		return fmt.Errorf("clone repository: %w%s%s", err, sanitizeGitOutput(out, cloneURL), credentialHint(out, authHeader))
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

// cloneBranchName maps a ref to what `git clone --branch` accepts: short
// branch/tag names only. Push events carry full refs (refs/heads/main,
// refs/tags/v1) — passed verbatim, every push-triggered run failed checkout.
func cloneBranchName(ref string) string {
	ref = strings.TrimSpace(ref)
	for _, prefix := range []string{"refs/heads/", "refs/tags/"} {
		if strings.HasPrefix(ref, prefix) {
			return strings.TrimPrefix(ref, prefix)
		}
	}
	return ref
}

// credentialHint says which side of a refused clone to look at. git reports a
// rejected credential and a missing one identically ("could not read Username"),
// so the run record has to say whether one was sent at all.
func credentialHint(out, authHeader string) string {
	if !strings.Contains(out, "could not read Username") && !strings.Contains(out, "Authentication failed") {
		return ""
	}
	if strings.TrimSpace(authHeader) == "" {
		return "\n(no clone credential was supplied for this run: the repository's git URL or run token was not configured when the run was queued)"
	}
	return "\n(a clone credential was supplied and the git server refused it after " + fmt.Sprint(cloneAttempts) + " attempts)"
}
