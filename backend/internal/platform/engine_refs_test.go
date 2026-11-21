package platform_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"testing"

	"github.com/nielsuitterdijk22/quill/internal/platform"
	"github.com/nielsuitterdijk22/quill/internal/workitemrefs"
)

// A pull request on a Quill-hosted repository that mentions a Tempo work-item key
// is pushed to Tempo through the outbox as it opens, closes, reopens and merges,
// with "merged" told apart from "closed". One that mentions no key sends nothing.
func TestEnginePullRequestsEnqueueWorkItemRefs(t *testing.T) {
	browseGitAvailable(t)
	svc, st, gitURL := browseFixture(t)
	svc = svc.WithWebURL("https://quill.example/")
	ctx := context.Background()
	owner := makeUser(t, st, "refs-owner")
	act := actor(owner)
	if _, err := svc.CreateProject(ctx, act, platform.CreateProjectInput{Slug: "refs-acme", Name: "acme"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateRepo(ctx, act, "refs-acme", platform.CreateRepoInput{Slug: "r", Name: "r"}); err != nil {
		t.Fatal(err)
	}
	work := t.TempDir()
	if out, err := exec.Command("git", "clone", "-q", gitURL+"/refs-acme/r", work).CombinedOutput(); err != nil {
		t.Fatalf("clone: %v\n%s", err, out)
	}
	commit := func(rel, msg string) {
		t.Helper()
		if err := os.WriteFile(work+"/"+rel, []byte(msg), 0o644); err != nil {
			t.Fatal(err)
		}
		browseGit(t, work, "add", "-A")
		browseGit(t, work, "commit", "-qm", msg)
	}
	push := func(args ...string) {
		t.Helper()
		if out, err := browseGitPush(t, work, args...); err != nil {
			t.Fatalf("push: %v\n%s", err, out)
		}
	}
	commit("a.txt", "base")
	push("origin", "main")
	for _, b := range []string{"rfx-7-docs", "plain"} {
		browseGit(t, work, "checkout", "-q", "-b", b, "main")
		commit(b+".txt", b)
		push("origin", b)
	}

	drain := func() []workitemrefs.Ref {
		t.Helper()
		rows, err := st.ListPendingWorkItemRefEvents(ctx, 100)
		if err != nil {
			t.Fatal(err)
		}
		var out []workitemrefs.Ref
		for _, r := range rows {
			var p workitemrefs.RefPush
			if err := json.Unmarshal(r.Payload, &p); err != nil {
				t.Fatal(err)
			}
			out = append(out, p.Refs...)
			if err := st.MarkWorkItemRefEventDelivered(ctx, r.ID); err != nil {
				t.Fatal(err)
			}
		}
		return out
	}
	drain() // anything left by other tests

	// A key in the branch name is enough; the title has none.
	if _, _, err := svc.CreatePull(ctx, act, "refs-acme", "r", platform.CreatePullInput{Title: "Add docs", Head: "rfx-7-docs", Base: "main"}); err != nil {
		t.Fatal(err)
	}
	got := drain()
	if len(got) != 1 || got[0].State != "open" || got[0].ExternalRef != "1" || got[0].WorkItemKeys[0] != "RFX-7" {
		t.Fatalf("after open: %+v", got)
	}
	if got[0].URL != "https://quill.example/projects/refs-acme/repos/r/pulls/1" {
		t.Fatalf("pull url = %q", got[0].URL)
	}

	// No key anywhere: nothing is sent.
	if _, _, err := svc.CreatePull(ctx, act, "refs-acme", "r", platform.CreatePullInput{Title: "Plain", Head: "plain", Base: "main"}); err != nil {
		t.Fatal(err)
	}
	if got := drain(); len(got) != 0 {
		t.Fatalf("a PR without keys must send nothing: %+v", got)
	}

	if _, _, err := svc.SetPullState(ctx, act, "refs-acme", "r", 1, "closed"); err != nil {
		t.Fatal(err)
	}
	if got := drain(); len(got) != 1 || got[0].State != "closed" {
		t.Fatalf("after close: %+v", got)
	}
	if _, _, err := svc.SetPullState(ctx, act, "refs-acme", "r", 1, "open"); err != nil {
		t.Fatal(err)
	}
	if got := drain(); len(got) != 1 || got[0].State != "open" {
		t.Fatalf("after reopen: %+v", got)
	}
	if _, _, err := svc.MergePull(ctx, act, "refs-acme", "r", 1, "squash"); err != nil {
		t.Fatal(err)
	}
	if got := drain(); len(got) != 1 || got[0].State != "merged" {
		t.Fatalf("after merge: %+v", got)
	}
}
