package platform_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/nielsuitterdijk22/quill/internal/platform"
)

func totalContributions(t *testing.T, svc *platform.Service, act platform.Actor) int {
	t.Helper()
	days, err := svc.MyContributions(context.Background(), act)
	if err != nil {
		t.Fatalf("MyContributions: %v", err)
	}
	n := 0
	for _, d := range days {
		if d.Timestamp%86400 != 0 {
			t.Fatalf("day bucket not at UTC midnight: %+v", d)
		}
		n += d.Contributions
	}
	return n
}

// The contribution graph (replacing Forgejo's heatmap) counts default-branch
// commits authored with the user's emails, indexed by backfill and then per
// push; extra commit emails attribute more commits; and an unverified email
// never reaches repositories the claimant cannot read.
func TestContributionsFromEngineCommits(t *testing.T) {
	browseGitAvailable(t)
	svc, st, gitURL := browseFixture(t)
	ctx := context.Background()
	const slug = "contrib"
	owner := makeUser(t, st, slug+"-owner") // email contrib-owner@example.test
	act := actor(owner)
	if _, err := svc.CreateProject(ctx, act, platform.CreateProjectInput{Slug: slug, Name: slug}); err != nil {
		t.Fatalf("project: %v", err)
	}
	repo, err := svc.CreateRepo(ctx, act, slug, platform.CreateRepoInput{Slug: "app", Name: "app"})
	if err != nil {
		t.Fatalf("repo: %v", err)
	}
	work := t.TempDir()
	if out, err := exec.Command("git", "clone", "-q", gitURL+"/"+slug+"/app", work).CombinedOutput(); err != nil {
		t.Fatalf("clone: %v\n%s", err, out)
	}
	commitAs := func(email, file string) string {
		os.WriteFile(filepath.Join(work, file), []byte(file+"\n"), 0o644)
		browseGit(t, work, "add", "-A")
		browseGit(t, work, "commit", "-qm", file, "--author=x <"+email+">")
		return browseGit(t, work, "rev-parse", "HEAD")
	}
	commitAs("Contrib-Owner@example.test", "a") // case-insensitive match
	commitAs("owner@work.test", "b")
	first := commitAs("someone-else@example.test", "c")
	if out, err := browseGitPush(t, work, "origin", "main"); err != nil {
		t.Fatalf("push: %v\n%s", err, out)
	}

	if n := totalContributions(t, svc, act); n != 0 {
		t.Fatalf("before indexing: %d contributions", n)
	}
	// Backfill indexes the repo's history once.
	if n, bad, err := svc.BackfillCommitActivity(ctx); err != nil || bad != 0 || n < 1 {
		t.Fatalf("backfill: indexed=%d failed=%d err=%v", n, bad, err)
	}
	if n := totalContributions(t, svc, act); n != 1 {
		t.Fatalf("after backfill: %d contributions, want 1 (account email only)", n)
	}
	if n, _, _ := svc.BackfillCommitActivity(ctx); n != 0 {
		t.Fatalf("second backfill re-indexed %d repos", n)
	}

	// An extra commit email attributes the work-address commit.
	got, err := svc.SetMyCommitEmails(ctx, act, []string{" Owner@Work.test ", "owner@work.test", "contrib-owner@example.test"})
	if err != nil || len(got.Emails) != 1 || got.Emails[0] != "owner@work.test" {
		t.Fatalf("set commit emails: %+v %v", got, err)
	}
	if n := totalContributions(t, svc, act); n != 2 {
		t.Fatalf("with commit email: %d contributions, want 2", n)
	}
	if _, err := svc.SetMyCommitEmails(ctx, act, []string{"not an email"}); !errors.Is(err, platform.ErrInvalidInput) {
		t.Fatalf("invalid email accepted: %v", err)
	}

	// A push indexes only what it brought.
	second := commitAs("owner@work.test", "d")
	if out, err := browseGitPush(t, work, "origin", "main"); err != nil {
		t.Fatalf("push: %v\n%s", err, out)
	}
	if _, err := svc.HandleEnginePush(ctx, repo.ID, []platform.EngineRefUpdate{{Ref: "refs/heads/main", Old: first, New: second}}); err != nil {
		t.Fatal(err)
	}
	if n := totalContributions(t, svc, act); n != 3 {
		t.Fatalf("after push: %d contributions, want 3", n)
	}

	// Claiming someone's email shows nothing of repos you cannot read.
	stranger := actor(makeUser(t, st, slug+"-stranger"))
	if _, err := svc.SetMyCommitEmails(ctx, stranger, []string{"contrib-owner@example.test", "owner@work.test"}); err != nil {
		t.Fatal(err)
	}
	if n := totalContributions(t, svc, stranger); n != 0 {
		t.Fatalf("stranger sees %d contributions of a private repo", n)
	}
}
