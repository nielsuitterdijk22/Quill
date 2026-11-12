package platform_test

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/nielsuitterdijk22/quill/internal/gitengine"
	"github.com/nielsuitterdijk22/quill/internal/objectstore"
	"github.com/nielsuitterdijk22/quill/internal/platform"
	"github.com/nielsuitterdijk22/quill/internal/store"
)

// B4 (serverless epic): engine-backed code browse. These integration tests
// drive the full production chain — own-mode provisioning, a REAL git push
// through the smart-HTTP write path (B3), then the browse reads served from
// the hydrated repo via upstream git — and prove routing, error semantics,
// and the public-read bypass work identically for engine repos and Forgejo
// repos. They need a git binary and are gated on QUILL_TEST_DATABASE_URL via
// newService.

// browseGit runs git in dir, failing the test on error.
func browseGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=quill-test", "GIT_AUTHOR_EMAIL=quill-test@example.com",
		"GIT_COMMITTER_NAME=quill-test", "GIT_COMMITTER_EMAIL=quill-test@example.com")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func browseGitAvailable(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not in PATH; skipping engine browse test")
	}
}

// browseFixture wires an own-engine platform service plus a smart-HTTP server
// that mirrors the production server wiring (resolve → hydrate; receive with
// the platform referee/guard/leases), returning the service and the git URL
// base to push into.
func browseFixture(t *testing.T) (*platform.Service, *store.Store, string) {
	t.Helper()
	mem := objectstore.NewMemStore()
	engine := gitengine.New(mem)
	cache := &gitengine.RepoCache{Root: t.TempDir(), Engine: engine, Packs: gitengine.NewBucketPackSource(mem)}
	svc, st := newService(t)
	svc = svc.WithGitEngine(engine).WithGitCache(cache)

	hydrate := func(ctx context.Context, owner, repo string) (string, error) {
		ref, err := svc.ResolveRepoForGit(ctx, owner, repo)
		if err != nil {
			return "", err
		}
		return cache.Hydrate(ctx, ref.TenantID.String(), ref.ProjectSlug, ref.RepoSlug)
	}
	resolveReceive := func(ctx context.Context, owner, repo string) (gitengine.ReceiveContext, error) {
		ref, err := svc.ResolveRepoForGit(ctx, owner, repo)
		if err != nil {
			return gitengine.ReceiveContext{}, err
		}
		dir, err := cache.Hydrate(ctx, ref.TenantID.String(), ref.ProjectSlug, ref.RepoSlug)
		if err != nil {
			return gitengine.ReceiveContext{}, err
		}
		return gitengine.ReceiveContext{
			ID: ref.RepoID.String(), Dir: dir,
			TenantID: ref.TenantID.String(), ProjectSlug: ref.ProjectSlug, RepoSlug: ref.RepoSlug,
		}, nil
	}
	h := &gitengine.SmartHTTP{
		Hydrate: hydrate,
		Receive: &gitengine.ReceiveConfig{
			Engine:  engine,
			Resolve: resolveReceive,
			Actor:   func(*http.Request) string { return "alice" },
			Referee: svc.GitReferee(),
			Guard:   svc.GitPushGuard(),
			Leases:  svc.GitPushLeases(),
		},
	}
	srv := httptest.NewServer(http.StripPrefix("/git", http.Handler(h)))
	t.Cleanup(srv.Close)
	return svc, st, srv.URL + "/git"
}

// browseGitPush runs git push in dir and returns output + error.
func browseGitPush(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"push"}, args...)...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=quill-test", "GIT_AUTHOR_EMAIL=quill-test@example.com",
		"GIT_COMMITTER_NAME=quill-test", "GIT_COMMITTER_EMAIL=quill-test@example.com")
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// TestEngineBrowsePushedRepo: create an own-engine repo, push real content
// through smart HTTP, then verify every browse op serves it from the engine —
// byte-compatible with the Forgejo DTO shapes the frontend consumes.
func TestEngineBrowsePushedRepo(t *testing.T) {
	browseGitAvailable(t)
	svc, st, gitURL := browseFixture(t)
	ctx := context.Background()
	owner := makeUser(t, st, "browser-owner")
	act := actor(owner)

	if _, err := svc.CreateProject(ctx, act, platform.CreateProjectInput{Slug: "browse-acme", Name: "acme"}); err != nil {
		t.Fatalf("create project: %v", err)
	}
	if _, err := svc.CreateRepo(ctx, act, "browse-acme", platform.CreateRepoInput{Slug: "browsed", Name: "browsed"}); err != nil {
		t.Fatalf("create repo: %v", err)
	}
	url := gitURL + "/browse-acme/browsed"

	// Clone, two commits, annotated tag, push.
	work := t.TempDir()
	if out, err := exec.Command("git", "clone", "-q", url, work).CombinedOutput(); err != nil {
		t.Fatalf("clone empty repo: %v\n%s", err, out)
	}
	writeCommit := func(rel, content, msg string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Join(work, filepath.Dir(rel)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(work, rel), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		browseGit(t, work, "add", "-A")
		browseGit(t, work, "commit", "-qm", msg)
	}
	writeCommit("README.md", "# browse fixture\nread me\n", "readme commit")
	writeCommit("docs/guide.md", "guide body\n", "docs commit")
	browseGit(t, work, "tag", "-a", "v1.0", "-m", "release")
	pushed := browseGit(t, work, "rev-parse", "HEAD")
	first := browseGit(t, work, "rev-parse", "HEAD~1")
	if out, err := browseGitPush(t, work, "origin", "main", "--tags"); err != nil {
		t.Fatalf("push: %v\n%s", err, out)
	}

	// ListBranches.
	_, branches, err := svc.ListBranches(ctx, act, "browse-acme", "browsed")
	if err != nil {
		t.Fatalf("ListBranches: %v", err)
	}
	if len(branches) != 1 || branches[0].Name != "main" {
		t.Fatalf("branches = %+v", branches)
	}
	if branches[0].Commit.ID != pushed || branches[0].Commit.Message != "docs commit" {
		t.Fatalf("branch tip = %+v, want %s docs commit", branches[0].Commit, pushed)
	}
	if branches[0].Commit.Author.Name != "quill-test" || branches[0].Commit.Author.Email != "quill-test@example.com" {
		t.Fatalf("branch actor = %+v", branches[0].Commit.Author)
	}
	if branches[0].Commit.Timestamp.IsZero() {
		t.Fatal("branch commit timestamp is zero")
	}

	// ListCommits on the default branch.
	_, commits, err := svc.ListCommits(ctx, act, "browse-acme", "browsed", "", "", 10)
	if err != nil {
		t.Fatalf("ListCommits: %v", err)
	}
	if len(commits) != 2 {
		t.Fatalf("commits = %d, want 2", len(commits))
	}
	if commits[0].SHA != pushed || commits[0].Commit.Message != "docs commit" {
		t.Fatalf("head commit = %+v", commits[0].Commit)
	}
	if commits[1].SHA != first || commits[1].Commit.Message != "readme commit" {
		t.Fatalf("second commit = %+v", commits[1].Commit)
	}
	if commits[0].Commit.Author.Name != "quill-test" || commits[0].Commit.Author.Date.IsZero() {
		t.Fatalf("commit author = %+v", commits[0].Commit.Author)
	}

	// Path-filtered log.
	_, onlyReadme, err := svc.ListCommits(ctx, act, "browse-acme", "browsed", "", "README.md", 10)
	if err != nil {
		t.Fatalf("ListCommits path: %v", err)
	}
	if len(onlyReadme) != 1 || onlyReadme[0].SHA != first {
		t.Fatalf("path-filtered commits = %+v", onlyReadme)
	}

	// GetCommit + diff.
	_, commit, files, err := svc.GetCommit(ctx, act, "browse-acme", "browsed", pushed)
	if err != nil {
		t.Fatalf("GetCommit: %v", err)
	}
	if commit.SHA != pushed || commit.Commit.Message != "docs commit" {
		t.Fatalf("commit = %+v", commit)
	}
	// The second commit adds exactly docs/guide.md against its first parent.
	if len(files) != 1 || files[0].Path != "docs/guide.md" {
		t.Fatalf("diff files = %+v", files)
	}

	// GetContents: root directory with per-entry last commits.
	_, contents, err := svc.GetContents(ctx, act, "browse-acme", "browsed", "", "")
	if err != nil {
		t.Fatalf("GetContents root: %v", err)
	}
	if !contents.IsDir || len(contents.Entries) != 2 {
		t.Fatalf("root contents = %+v", contents)
	}
	var gotReadme, gotDocs bool
	for i := range contents.Entries {
		e := &contents.Entries[i]
		switch e.Name {
		case "README.md":
			gotReadme = true
			if e.Type != "file" || e.Size == 0 || e.Path != "README.md" {
				t.Fatalf("README entry = %+v", e)
			}
			if e.LastCommit == nil || e.LastCommit.SHA != first ||
				e.LastCommit.Message != "readme commit" || e.LastCommit.AuthorName != "quill-test" {
				t.Fatalf("README last commit = %+v, want the first commit", e.LastCommit)
			}
		case "docs":
			gotDocs = true
			if e.Type != "dir" {
				t.Fatalf("docs entry type = %q, want dir", e.Type)
			}
			if e.LastCommit == nil || e.LastCommit.SHA != pushed {
				t.Fatalf("docs last commit = %+v, want the tip", e.LastCommit)
			}
		default:
			t.Fatalf("unexpected root entry %+v", e)
		}
	}
	if !gotReadme || !gotDocs {
		t.Fatalf("root listing missing entries (readme=%v docs=%v): %+v", gotReadme, gotDocs, contents.Entries)
	}

	// GetContents: single file as base64.
	_, file, err := svc.GetContents(ctx, act, "browse-acme", "browsed", "README.md", "")
	if err != nil {
		t.Fatalf("GetContents file: %v", err)
	}
	if file.IsDir || file.File == nil {
		t.Fatalf("file contents = %+v", file)
	}
	if file.File.Encoding == nil || *file.File.Encoding != "base64" || file.File.Content == nil {
		t.Fatalf("file encoding/content missing: %+v", file.File)
	}
	decoded, err := base64.StdEncoding.DecodeString(*file.File.Content)
	if err != nil {
		t.Fatalf("base64 decode: %v", err)
	}
	if string(decoded) != "# browse fixture\nread me\n" {
		t.Fatalf("file content = %q", decoded)
	}
	if file.File.Type != "file" || file.File.SHA == "" || file.File.Size != int64(len(decoded)) {
		t.Fatalf("file entry = %+v", file.File)
	}

	// Nested path at a non-default ref (the tag).
	_, atTag, err := svc.GetContents(ctx, act, "browse-acme", "browsed", "", "v1.0")
	if err != nil {
		t.Fatalf("GetContents at tag: %v", err)
	}
	if !atTag.IsDir {
		t.Fatalf("tag contents = %+v", atTag)
	}

	// Missing path → ErrNotFound, unknown ref → ErrNotFound, unknown sha → ErrNotFound.
	if _, _, err := svc.GetContents(ctx, act, "browse-acme", "browsed", "nope.txt", ""); err != platform.ErrNotFound {
		t.Fatalf("missing path = %v, want ErrNotFound", err)
	}
	if _, _, err := svc.ListCommits(ctx, act, "browse-acme", "browsed", "ghost-branch", "", 10); err != platform.ErrNotFound {
		t.Fatalf("unknown ref = %v, want ErrNotFound", err)
	}
	fake := strings.Repeat("1", 40)
	if _, _, _, err := svc.GetCommit(ctx, act, "browse-acme", "browsed", fake); err != platform.ErrNotFound {
		t.Fatalf("unknown sha = %v, want ErrNotFound", err)
	}

	// RenderMarkdown on an engine repo: GFM features render, raw HTML stays
	// escaped (goldmark default: no WithUnsafe).
	html, err := svc.RenderMarkdown(ctx, act, "browse-acme", "browsed", "| a | b |\n|---|---|\n| 1 | 2 |\n")
	if err != nil {
		t.Fatalf("RenderMarkdown: %v", err)
	}
	if !strings.Contains(html, "<table>") {
		t.Fatalf("GFM table did not render:\n%s", html)
	}
	html, err = svc.RenderMarkdown(ctx, act, "browse-acme", "browsed", "hello <script>alert(1)</script>")
	if err != nil {
		t.Fatalf("RenderMarkdown html: %v", err)
	}
	if strings.Contains(html, "<script>") {
		t.Fatalf("raw HTML must be escaped:\n%s", html)
	}
}

// TestEngineBrowsePublicReadAndEdges covers the remaining routing semantics:
// the public-read bypass for anonymous actors, the empty-repo sentinel, and
// the discriminator — a repo WITHOUT an engine referee row routes to the
// Forgejo backend (here disabled), proving not every repo goes engine-side.
func TestEngineBrowsePublicReadAndEdges(t *testing.T) {
	browseGitAvailable(t)
	svc, st, gitURL := browseFixture(t)
	ctx := context.Background()
	owner := makeUser(t, st, "browse-pub-owner")

	if _, err := svc.CreateProject(ctx, actor(owner), platform.CreateProjectInput{Slug: "browse-pub", Name: "pub"}); err != nil {
		t.Fatalf("create project: %v", err)
	}
	if _, err := svc.CreateRepo(ctx, actor(owner), "browse-pub", platform.CreateRepoInput{Slug: "pubrepo", Name: "pubrepo", Visibility: "public"}); err != nil {
		t.Fatalf("create public repo: %v", err)
	}
	if _, err := svc.CreateRepo(ctx, actor(owner), "browse-pub", platform.CreateRepoInput{Slug: "empty-repo", Name: "empty"}); err != nil {
		t.Fatalf("create empty repo: %v", err)
	}

	// Anonymous browse of a public engine repo works without any git being
	// pushed (empty repo list semantics).
	anon := platform.Actor{UserID: uuid.Nil}
	if _, branches, err := svc.ListBranches(ctx, anon, "browse-pub", "pubrepo"); err != nil || len(branches) != 0 {
		t.Fatalf("anonymous ListBranches on public repo = %v, %d branches", err, len(branches))
	}

	// Empty repo: commits and contents report ErrEmptyRepo, not NotFound.
	if _, _, err := svc.ListCommits(ctx, anon, "browse-pub", "pubrepo", "", "", 10); err != platform.ErrEmptyRepo {
		t.Fatalf("empty repo commits = %v, want ErrEmptyRepo", err)
	}
	if _, _, err := svc.GetContents(ctx, anon, "browse-pub", "pubrepo", "", ""); err != platform.ErrEmptyRepo {
		t.Fatalf("empty repo contents = %v, want ErrEmptyRepo", err)
	}

	// Anonymous browse of a private repo never reveals it: NotFound.
	if _, err := svc.CreateRepo(ctx, actor(owner), "browse-pub", platform.CreateRepoInput{Slug: "privrepo", Name: "priv"}); err != nil {
		t.Fatalf("create private repo: %v", err)
	}
	if _, _, err := svc.ListCommits(ctx, anon, "browse-pub", "privrepo", "", "", 10); err != platform.ErrNotFound {
		t.Fatalf("anonymous private repo = %v, want ErrNotFound", err)
	}

	// Discriminator: strip the referee row from the private repo and it must
	// route to the (disabled) Forgejo backend — ErrUnavailable, not an engine
	// answer. This is exactly the Phase-D migration discriminator inverted.
	if _, err := st.Pool().Exec(ctx, "DELETE FROM git_repo_state WHERE repo_id IN "+
		"(SELECT id FROM repositories WHERE slug IN ('privrepo'))"); err != nil {
		t.Fatalf("strip referee row: %v", err)
	}
	if _, _, err := svc.ListCommits(ctx, actor(owner), "browse-pub", "privrepo", "", "", 10); err != platform.ErrUnavailable {
		t.Fatalf("forgejo-routed repo = %v, want ErrUnavailable (forgejo disabled)", err)
	}

	// The engine repo clone URL is served: sanity that the mounted git path
	// still works after the routing change (public clone of the empty repo).
	if out, err := exec.Command("git", "ls-remote", gitURL+"/browse-pub/pubrepo").CombinedOutput(); err != nil {
		t.Fatalf("git ls-remote: %v\n%s", err, out)
	}
}
