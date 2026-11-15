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

// PR and issue descriptions are editable (2026-11-15; before, a PR's title
// and body were fixed at creation and an issue's body too). Members edit;
// outsiders can't; a title can't be blanked; an empty description clears it.
func TestEditPullAndIssueText(t *testing.T) {
	browseGitAvailable(t)
	svc, st, gitURL := browseFixture(t)
	ctx := context.Background()
	act := actor(makeUser(t, st, "edit-owner"))
	stranger := actor(makeUser(t, st, "edit-stranger"))
	if _, err := svc.CreateProject(ctx, act, platform.CreateProjectInput{Slug: "edit-acme", Name: "acme"}); err != nil {
		t.Fatalf("project: %v", err)
	}
	if _, err := svc.CreateRepo(ctx, act, "edit-acme", platform.CreateRepoInput{Slug: "app", Name: "app"}); err != nil {
		t.Fatalf("repo: %v", err)
	}
	work := t.TempDir()
	if out, err := exec.Command("git", "clone", "-q", gitURL+"/edit-acme/app", work).CombinedOutput(); err != nil {
		t.Fatalf("clone: %v\n%s", err, out)
	}
	os.WriteFile(filepath.Join(work, "a.txt"), []byte("a\n"), 0o644)
	browseGit(t, work, "add", "-A")
	browseGit(t, work, "commit", "-qm", "base")
	if out, err := browseGitPush(t, work, "origin", "main"); err != nil {
		t.Fatalf("push main: %v\n%s", err, out)
	}
	browseGit(t, work, "checkout", "-qb", "feature")
	os.WriteFile(filepath.Join(work, "b.txt"), []byte("b\n"), 0o644)
	browseGit(t, work, "add", "-A")
	browseGit(t, work, "commit", "-qm", "feature")
	if out, err := browseGitPush(t, work, "origin", "feature"); err != nil {
		t.Fatalf("push feature: %v\n%s", err, out)
	}
	_, pr, err := svc.CreatePull(ctx, act, "edit-acme", "app", platform.CreatePullInput{Title: "wip", Body: "draft", Head: "feature", Base: "main"})
	if err != nil {
		t.Fatalf("CreatePull: %v", err)
	}
	n := int(pr.Number)

	title, body := "Add b", "## Why\n\nBecause.\n\n"
	_, got, err := svc.EditPull(ctx, act, "edit-acme", "app", n, &title, &body)
	if err != nil {
		t.Fatalf("EditPull: %v", err)
	}
	if got.Title != "Add b" || got.Body != "## Why\n\nBecause." {
		t.Fatalf("edited pull = %q / %q", got.Title, got.Body)
	}
	// Body only: the title stays.
	empty := ""
	if _, got, err = svc.EditPull(ctx, act, "edit-acme", "app", n, nil, &empty); err != nil || got.Title != "Add b" || got.Body != "" {
		t.Fatalf("clear body: %+v %v", got, err)
	}
	if _, _, err := svc.EditPull(ctx, act, "edit-acme", "app", n, &empty, nil); !errors.Is(err, platform.ErrInvalidInput) {
		t.Fatalf("blank title: %v", err)
	}
	if _, _, err := svc.EditPull(ctx, stranger, "edit-acme", "app", n, &title, &body); err == nil {
		t.Fatal("a non-member edited the pull")
	}

	// Close and reopen (an API client closes a superseded PR).
	if _, got, err := svc.SetPullState(ctx, act, "edit-acme", "app", n, "closed"); err != nil || got.State != "closed" {
		t.Fatalf("close: %+v %v", got.State, err)
	}
	if _, got, err := svc.SetPullState(ctx, act, "edit-acme", "app", n, "open"); err != nil || got.State != "open" {
		t.Fatalf("reopen: %+v %v", got.State, err)
	}
	if _, _, err := svc.SetPullState(ctx, stranger, "edit-acme", "app", n, "closed"); err == nil {
		t.Fatal("a non-member closed the pull")
	}

	is, err := svc.CreateIssue(ctx, act, "edit-acme", "app", "Bug", "old")
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	if got, err := svc.EditIssueBody(ctx, act, "edit-acme", "app", int(is.Number), "new *text*"); err != nil || got.Body != "new *text*" || got.Title != "Bug" {
		t.Fatalf("EditIssueBody: %+v %v", got, err)
	}
	if _, err := svc.EditIssueBody(ctx, stranger, "edit-acme", "app", int(is.Number), "x"); err == nil {
		t.Fatal("a non-member edited the issue")
	}
}
