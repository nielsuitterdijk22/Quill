package forgejo_test

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/nielsuitterdijk22/quill/internal/config"
	"github.com/nielsuitterdijk22/quill/internal/forgejo"
)

// newClient returns a Forgejo client against a live instance, or skips when the
// test env vars are unset so CI without Forgejo stays green.
func newClient(t *testing.T) *forgejo.Client {
	t.Helper()
	base := os.Getenv("QUILL_TEST_FORGEJO_URL")
	token := os.Getenv("QUILL_TEST_FORGEJO_TOKEN")
	if base == "" || token == "" {
		t.Skip("QUILL_TEST_FORGEJO_URL/QUILL_TEST_FORGEJO_TOKEN not set; skipping forgejo integration test")
	}
	c := forgejo.New(config.ForgejoConfig{BaseURL: base, AdminToken: token})
	if !c.Enabled() {
		t.Fatal("client should be enabled with base + token set")
	}
	return c
}

func TestForgejoVersion(t *testing.T) {
	c := newClient(t)
	v, err := c.Version(context.Background())
	if err != nil {
		t.Fatalf("version: %v", err)
	}
	if v == "" {
		t.Fatal("expected a non-empty version")
	}
}

func TestForgejoUserLifecycle(t *testing.T) {
	c := newClient(t)
	ctx := context.Background()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	username := "quill-itest-u-" + suffix

	created, err := c.CreateUser(ctx, forgejo.CreateUserOptions{
		Username: username,
		Email:    username + "@quill.test",
		Password: "Quill-Itest-" + suffix,
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	t.Cleanup(func() { _ = c.DeleteUser(context.Background(), username, true) })
	if created.Login != username {
		t.Fatalf("login mismatch: got %q want %q", created.Login, username)
	}

	got, err := c.GetUser(ctx, username)
	if err != nil {
		t.Fatalf("get user: %v", err)
	}
	if got.ID != created.ID {
		t.Fatalf("id mismatch: got %d want %d", got.ID, created.ID)
	}

	if err := c.DeleteUser(ctx, username, true); err != nil {
		t.Fatalf("delete user: %v", err)
	}
	if _, err := c.GetUser(ctx, username); !forgejo.NotFound(err) {
		t.Fatalf("expected NotFound after delete, got %v", err)
	}
}

func TestForgejoOrgAndRepoLifecycle(t *testing.T) {
	c := newClient(t)
	ctx := context.Background()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	orgName := "quill-itest-o-" + suffix
	repoName := "quill-itest-r-" + suffix

	org, err := c.CreateOrg(ctx, forgejo.CreateOrgOptions{Name: orgName, FullName: "Quill ITest Org", Visibility: "private"})
	if err != nil {
		t.Fatalf("create org: %v", err)
	}
	t.Cleanup(func() { _ = c.DeleteOrg(context.Background(), orgName) })
	if org.Handle() != orgName {
		t.Fatalf("org handle mismatch: got %q want %q", org.Handle(), orgName)
	}

	repo, err := c.CreateOrgRepo(ctx, orgName, forgejo.CreateRepoOptions{
		Name:          repoName,
		Private:       true,
		AutoInit:      true,
		DefaultBranch: "main",
	})
	if err != nil {
		t.Fatalf("create repo: %v", err)
	}
	t.Cleanup(func() { _ = c.DeleteRepo(context.Background(), orgName, repoName) })
	if repo.Name != repoName {
		t.Fatalf("repo name mismatch: got %q want %q", repo.Name, repoName)
	}
	if repo.Empty {
		t.Fatal("auto-initialised repo should not be empty")
	}

	got, err := c.GetRepo(ctx, orgName, repoName)
	if err != nil {
		t.Fatalf("get repo: %v", err)
	}
	if got.ID != repo.ID {
		t.Fatalf("repo id mismatch: got %d want %d", got.ID, repo.ID)
	}

	if err := c.DeleteRepo(ctx, orgName, repoName); err != nil {
		t.Fatalf("delete repo: %v", err)
	}
	if err := c.DeleteOrg(ctx, orgName); err != nil {
		t.Fatalf("delete org: %v", err)
	}
	if _, err := c.GetOrg(ctx, orgName); !forgejo.NotFound(err) {
		t.Fatalf("expected NotFound after org delete, got %v", err)
	}
}

func TestForgejoBrowse(t *testing.T) {
	c := newClient(t)
	ctx := context.Background()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	orgName := "quill-itest-b-" + suffix
	repoName := "quill-itest-br-" + suffix

	if _, err := c.CreateOrg(ctx, forgejo.CreateOrgOptions{Name: orgName, Visibility: "private"}); err != nil {
		t.Fatalf("create org: %v", err)
	}
	t.Cleanup(func() { _ = c.DeleteOrg(context.Background(), orgName) })
	if _, err := c.CreateOrgRepo(ctx, orgName, forgejo.CreateRepoOptions{
		Name:          repoName,
		Private:       true,
		AutoInit:      true,
		DefaultBranch: "main",
	}); err != nil {
		t.Fatalf("create repo: %v", err)
	}
	t.Cleanup(func() { _ = c.DeleteRepo(context.Background(), orgName, repoName) })

	branches, err := c.ListBranches(ctx, orgName, repoName)
	if err != nil {
		t.Fatalf("list branches: %v", err)
	}
	if !hasBranch(branches, "main") {
		t.Fatalf("expected a main branch, got %+v", branches)
	}

	// Root listing should contain the auto-init README.
	root, err := c.GetContents(ctx, orgName, repoName, "", "main")
	if err != nil {
		t.Fatalf("get root contents: %v", err)
	}
	if !root.IsDir {
		t.Fatal("root contents should be a directory")
	}
	if !hasEntry(root.Entries, "README.md") {
		t.Fatalf("expected README.md in root, got %+v", root.Entries)
	}

	// Fetching the README directly returns its base64-encoded content.
	file, err := c.GetContents(ctx, orgName, repoName, "README.md", "main")
	if err != nil {
		t.Fatalf("get README: %v", err)
	}
	if file.IsDir || file.File == nil {
		t.Fatal("README should resolve to a file")
	}
	if file.File.Content == nil || *file.File.Content == "" {
		t.Fatal("README file should carry base64 content")
	}

	// A missing path must surface as a 404.
	if _, err := c.GetContents(ctx, orgName, repoName, "does/not/exist", "main"); !forgejo.NotFound(err) {
		t.Fatalf("expected NotFound for missing path, got %v", err)
	}

	commits, err := c.ListCommits(ctx, orgName, repoName, "main", "", 10)
	if err != nil {
		t.Fatalf("list commits: %v", err)
	}
	if len(commits) == 0 {
		t.Fatal("expected at least one commit")
	}
	if commits[0].SHA == "" {
		t.Fatal("commit should carry a SHA")
	}
}

func TestForgejoPulls(t *testing.T) {
	c := newClient(t)
	ctx := context.Background()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	orgName := "quill-itest-p-" + suffix
	repoName := "quill-itest-pr-" + suffix
	userName := "quill-itest-pu-" + suffix

	if _, err := c.CreateOrg(ctx, forgejo.CreateOrgOptions{Name: orgName, Visibility: "private"}); err != nil {
		t.Fatalf("create org: %v", err)
	}
	t.Cleanup(func() { _ = c.DeleteOrg(context.Background(), orgName) })
	if _, err := c.CreateOrgRepo(ctx, orgName, forgejo.CreateRepoOptions{
		Name:          repoName,
		Private:       true,
		AutoInit:      true,
		DefaultBranch: "main",
	}); err != nil {
		t.Fatalf("create repo: %v", err)
	}
	t.Cleanup(func() { _ = c.DeleteRepo(context.Background(), orgName, repoName) })

	// A user to attribute the PR to (via sudo). Forgejo requires the user to
	// have access, so grant them collaborator rights first.
	if _, err := c.CreateUser(ctx, forgejo.CreateUserOptions{
		Username: userName,
		Email:    userName + "@quill.test",
		Password: "Quill-Itest-" + suffix,
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	t.Cleanup(func() { _ = c.DeleteUser(context.Background(), userName, true) })
	if err := c.AddCollaborator(ctx, orgName, repoName, userName, "write"); err != nil {
		t.Fatalf("add collaborator: %v", err)
	}

	// Create a divergent commit on a feature branch so a PR has changes.
	if err := c.CreateBranch(ctx, orgName, repoName, "feature", "main"); err != nil {
		t.Fatalf("create branch: %v", err)
	}
	if err := c.CreateFile(ctx, orgName, repoName, "feature.txt", []byte("hello from the feature branch\n"), "Add feature.txt", "feature"); err != nil {
		t.Fatalf("create file: %v", err)
	}

	pr, err := c.CreatePull(ctx, orgName, repoName, userName, forgejo.CreatePullOptions{
		Title: "Add a feature file",
		Body:  "Introduces feature.txt.",
		Head:  "feature",
		Base:  "main",
	})
	if err != nil {
		t.Fatalf("create pull: %v", err)
	}
	if pr.Number == 0 {
		t.Fatal("expected a non-zero PR number")
	}
	if pr.User == nil || pr.User.Login != userName {
		t.Fatalf("expected PR attributed to %q, got %+v", userName, pr.User)
	}

	got, err := c.GetPull(ctx, orgName, repoName, pr.Number)
	if err != nil {
		t.Fatalf("get pull: %v", err)
	}
	if got.Title != "Add a feature file" {
		t.Fatalf("title mismatch: %q", got.Title)
	}

	pulls, err := c.ListPulls(ctx, orgName, repoName, "open", 0)
	if err != nil {
		t.Fatalf("list pulls: %v", err)
	}
	if len(pulls) != 1 || pulls[0].Number != pr.Number {
		t.Fatalf("expected one open PR #%d, got %+v", pr.Number, pulls)
	}

	diff, err := c.GetPullDiff(ctx, orgName, repoName, pr.Number)
	if err != nil {
		t.Fatalf("get diff: %v", err)
	}
	files := forgejo.ParseUnifiedDiff(diff)
	if !hasDiffFile(files, "feature.txt") {
		t.Fatalf("expected feature.txt in diff, got %+v", files)
	}

	comment, err := c.CreateIssueComment(ctx, orgName, repoName, pr.Number, userName, "Looks good to me.")
	if err != nil {
		t.Fatalf("create comment: %v", err)
	}
	if comment.User == nil || comment.User.Login != userName {
		t.Fatalf("expected comment attributed to %q, got %+v", userName, comment.User)
	}
	comments, err := c.ListIssueComments(ctx, orgName, repoName, pr.Number)
	if err != nil {
		t.Fatalf("list comments: %v", err)
	}
	if len(comments) != 1 {
		t.Fatalf("expected one comment, got %d", len(comments))
	}

	if err := c.MergePull(ctx, orgName, repoName, pr.Number, userName, forgejo.MergePullOptions{Do: "squash"}); err != nil {
		t.Fatalf("merge pull: %v", err)
	}
	merged, err := c.GetPull(ctx, orgName, repoName, pr.Number)
	if err != nil {
		t.Fatalf("get merged pull: %v", err)
	}
	if !merged.Merged || merged.State != "closed" {
		t.Fatalf("expected merged+closed, got merged=%v state=%q", merged.Merged, merged.State)
	}
}

func hasDiffFile(files []forgejo.DiffFile, path string) bool {
	for _, f := range files {
		if f.Path == path {
			return true
		}
	}
	return false
}

func hasBranch(branches []forgejo.Branch, name string) bool {
	for _, b := range branches {
		if b.Name == name {
			return true
		}
	}
	return false
}

func hasEntry(entries []forgejo.ContentEntry, name string) bool {
	for _, e := range entries {
		if e.Name == name {
			return true
		}
	}
	return false
}

// TestForgejoBranchProtectionAndReviews exercises the Forgejo surface Quill's
// branch policies rely on: branch protection upsert/get/delete and pull-request
// review submission/listing. It touches only Forgejo (no Quill Postgres), so it
// is safe to run alongside the demo database.
func TestForgejoBranchProtectionAndReviews(t *testing.T) {
	c := newClient(t)
	ctx := context.Background()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	orgName := "quill-itest-bp-" + suffix
	repoName := "quill-itest-bpr-" + suffix
	authorName := "quill-itest-bpa-" + suffix
	reviewerName := "quill-itest-bpv-" + suffix

	if _, err := c.CreateOrg(ctx, forgejo.CreateOrgOptions{Name: orgName, Visibility: "private"}); err != nil {
		t.Fatalf("create org: %v", err)
	}
	t.Cleanup(func() { _ = c.DeleteOrg(context.Background(), orgName) })
	if _, err := c.CreateOrgRepo(ctx, orgName, forgejo.CreateRepoOptions{
		Name:          repoName,
		Private:       true,
		AutoInit:      true,
		DefaultBranch: "main",
	}); err != nil {
		t.Fatalf("create repo: %v", err)
	}
	t.Cleanup(func() { _ = c.DeleteRepo(context.Background(), orgName, repoName) })

	// Branch protection: require one approval and forbid direct pushes to main.
	if err := c.UpsertBranchProtection(ctx, orgName, repoName, forgejo.BranchProtectionOptions{
		BranchName:             "main",
		EnablePush:             false,
		RequiredApprovals:      1,
		DismissStaleApprovals:  true,
		BlockOnRejectedReviews: true,
	}); err != nil {
		t.Fatalf("upsert branch protection: %v", err)
	}
	prot, ok, err := c.GetBranchProtection(ctx, orgName, repoName, "main")
	if err != nil {
		t.Fatalf("get branch protection: %v", err)
	}
	if !ok {
		t.Fatal("expected branch protection to exist")
	}
	if prot.RequiredApprovals != 1 || prot.EnablePush {
		t.Fatalf("unexpected protection: approvals=%d enablePush=%v", prot.RequiredApprovals, prot.EnablePush)
	}

	// Upsert again with new settings to prove the update (PATCH) path works.
	if err := c.UpsertBranchProtection(ctx, orgName, repoName, forgejo.BranchProtectionOptions{
		BranchName:        "main",
		EnablePush:        false,
		RequiredApprovals: 2,
	}); err != nil {
		t.Fatalf("update branch protection: %v", err)
	}
	prot, _, err = c.GetBranchProtection(ctx, orgName, repoName, "main")
	if err != nil {
		t.Fatalf("get updated protection: %v", err)
	}
	if prot.RequiredApprovals != 2 {
		t.Fatalf("expected 2 required approvals after update, got %d", prot.RequiredApprovals)
	}

	// A PR author and a separate reviewer (Forgejo forbids self-approval and
	// requires reviewers to have repo access).
	for _, u := range []string{authorName, reviewerName} {
		if _, err := c.CreateUser(ctx, forgejo.CreateUserOptions{
			Username: u,
			Email:    u + "@quill.test",
			Password: "Quill-Itest-" + suffix,
		}); err != nil {
			t.Fatalf("create user %s: %v", u, err)
		}
		user := u
		t.Cleanup(func() { _ = c.DeleteUser(context.Background(), user, true) })
		if err := c.AddCollaborator(ctx, orgName, repoName, u, "write"); err != nil {
			t.Fatalf("add collaborator %s: %v", u, err)
		}
	}

	if err := c.CreateBranch(ctx, orgName, repoName, "feature", "main"); err != nil {
		t.Fatalf("create branch: %v", err)
	}
	if err := c.CreateFile(ctx, orgName, repoName, "feature.txt", []byte("change\n"), "Add feature.txt", "feature"); err != nil {
		t.Fatalf("create file: %v", err)
	}
	pr, err := c.CreatePull(ctx, orgName, repoName, authorName, forgejo.CreatePullOptions{
		Title: "Feature",
		Head:  "feature",
		Base:  "main",
	})
	if err != nil {
		t.Fatalf("create pull: %v", err)
	}

	// The reviewer approves; the review must come back in the listing.
	review, err := c.CreateReview(ctx, orgName, repoName, pr.Number, reviewerName, forgejo.CreateReviewOptions{
		Event: forgejo.ReviewApproved,
		Body:  "LGTM",
	})
	if err != nil {
		t.Fatalf("create review: %v", err)
	}
	if review.State != forgejo.ReviewApproved {
		t.Fatalf("expected APPROVED review, got %q", review.State)
	}
	reviews, err := c.ListReviews(ctx, orgName, repoName, pr.Number)
	if err != nil {
		t.Fatalf("list reviews: %v", err)
	}
	if !hasApprovalFrom(reviews, reviewerName) {
		t.Fatalf("expected an APPROVED review from %q, got %+v", reviewerName, reviews)
	}

	// Deleting protection should leave none behind.
	if err := c.DeleteBranchProtection(ctx, orgName, repoName, "main"); err != nil {
		t.Fatalf("delete branch protection: %v", err)
	}
	if _, ok, err := c.GetBranchProtection(ctx, orgName, repoName, "main"); err != nil || ok {
		t.Fatalf("expected protection gone, ok=%v err=%v", ok, err)
	}
}

func hasApprovalFrom(reviews []forgejo.Review, login string) bool {
	for _, r := range reviews {
		if r.State == forgejo.ReviewApproved && r.User != nil && r.User.Login == login {
			return true
		}
	}
	return false
}
