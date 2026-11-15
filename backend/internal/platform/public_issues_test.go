package platform_test

import (
	"context"
	"errors"
	"testing"

	"github.com/nielsuitterdijk22/quill/internal/platform"
)

// On a public repository any signed-in user may read, file and comment on
// issues (Quill's support link points at its own public tracker); editing
// stays with project members, and private repositories stay closed.
func TestPublicRepoIssuesOpenToSignedInUsers(t *testing.T) {
	browseGitAvailable(t)
	svc, st, _ := browseFixture(t)
	ctx := context.Background()
	owner := actor(makeUser(t, st, "pubiss-owner"))
	stranger := actor(makeUser(t, st, "pubiss-stranger"))
	if _, err := svc.CreateProject(ctx, owner, platform.CreateProjectInput{Slug: "pubiss", Name: "pubiss"}); err != nil {
		t.Fatal(err)
	}
	for _, r := range []struct{ slug, vis string }{{"open", "public"}, {"closed", "private"}} {
		if _, err := svc.CreateRepo(ctx, owner, "pubiss", platform.CreateRepoInput{Slug: r.slug, Name: r.slug, Visibility: r.vis}); err != nil {
			t.Fatalf("repo %s: %v", r.slug, err)
		}
	}

	is, err := svc.CreateIssue(ctx, stranger, "pubiss", "open", "Bug: something", "steps")
	if err != nil {
		t.Fatalf("stranger files an issue on a public repo: %v", err)
	}
	if _, err := svc.CreateIssueComment(ctx, stranger, "pubiss", "open", int(is.Number), "more detail"); err != nil {
		t.Fatalf("stranger comments: %v", err)
	}
	if list, err := svc.ListIssues(ctx, stranger, "pubiss", "open", "open", 1); err != nil || len(list) != 1 {
		t.Fatalf("stranger lists: %v %v", len(list), err)
	}
	if _, _, err := svc.GetIssue(ctx, stranger, "pubiss", "open", int(is.Number)); err != nil {
		t.Fatalf("stranger reads: %v", err)
	}
	closed := "closed"
	if _, err := svc.EditIssue(ctx, stranger, "pubiss", "open", int(is.Number), &closed, nil); !errors.Is(err, platform.ErrForbidden) {
		t.Fatalf("stranger edited an issue: %v", err)
	}
	if _, err := svc.EditIssue(ctx, owner, "pubiss", "open", int(is.Number), &closed, nil); err != nil {
		t.Fatalf("owner closes: %v", err)
	}
	if _, err := svc.CreateIssue(ctx, stranger, "pubiss", "closed", "x", ""); !errors.Is(err, platform.ErrForbidden) {
		t.Fatalf("stranger filed on a private repo: %v", err)
	}
	if _, err := svc.ListIssues(ctx, stranger, "pubiss", "closed", "open", 1); !errors.Is(err, platform.ErrForbidden) {
		t.Fatalf("stranger listed a private repo: %v", err)
	}
}
