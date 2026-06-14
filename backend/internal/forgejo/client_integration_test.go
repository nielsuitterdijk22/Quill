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
