package gitengine

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nielsuitterdijk22/quill/internal/objectstore"
)

// gitCmd runs git in dir, failing the test on error; output returned.
func gitCmd(t *testing.T, dir string, args ...string) string {
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
	return string(out)
}

// gitAvailable skips the smart-HTTP tests when git is missing.
func gitAvailable(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not in PATH; skipping git-backed test")
	}
}

// seedFixtureRepo builds a real repo (a commit with a file + a tag), packs it,
// uploads pack + idx to the object store and returns the matching manifest.
// Thin test wrapper over the exported SeedFixture (shared with platform tests).
func seedFixtureRepo(t *testing.T, store ObjectStore, tenantID, projectSlug, repoSlug string) *Manifest {
	t.Helper()
	gitAvailable(t)
	m, err := SeedFixture(context.Background(), store, t.TempDir(), tenantID, projectSlug, repoSlug)
	if err != nil {
		t.Fatalf("SeedFixture: %v", err)
	}
	return m
}

// newTestCache wires an engine over a fresh memstore + temp cache dir.
func newTestCache(t *testing.T) (*objectstore.MemStore, *RepoCache) {
	t.Helper()
	mem := objectstore.NewMemStore()
	return mem, &RepoCache{
		Root:   t.TempDir(),
		Engine: New(mem),
		Packs:  NewBucketPackSource(mem),
	}
}

func TestHydrateMaterializesBareRepo(t *testing.T) {
	gitAvailable(t)
	mem, cache := newTestCache(t)
	m := seedFixtureRepo(t, mem, "t1", "acme", "widgets")
	ctx := context.Background()

	dir, err := cache.Hydrate(ctx, "t1", "acme", "widgets")
	if err != nil {
		t.Fatalf("Hydrate: %v", err)
	}
	// HEAD points at the manifest's default branch.
	head, err := os.ReadFile(filepath.Join(dir, "HEAD"))
	if err != nil || string(head) != "ref: refs/heads/main\n" {
		t.Fatalf("HEAD = %q %v", head, err)
	}
	// packed-refs carries every manifest ref (sorted).
	refs, err := os.ReadFile(filepath.Join(dir, "packed-refs"))
	if err != nil {
		t.Fatalf("packed-refs: %v", err)
	}
	for _, r := range m.Refs {
		if !strings.Contains(string(refs), r.Name+" "+r.Target) &&
			!strings.Contains(string(refs), r.Target+" "+r.Name) {
			t.Fatalf("packed-refs %q missing %v", refs, r)
		}
	}
	// git accepts the hydrated repo and the fixture objects resolve.
	out := gitCmd(t, dir, "cat-file", "-t", m.Refs[0].Target)
	if strings.TrimSpace(out) != "commit" {
		t.Fatalf("cat-file commit = %q", out)
	}
}

// countingStore wraps an ObjectStore and counts reads — hydration must not
// re-fetch packs it already hydrated.
type countingStore struct {
	ObjectStore
	gets map[string]int
}

func (c *countingStore) Get(ctx context.Context, key string) ([]byte, string, error) {
	if c.gets == nil {
		c.gets = map[string]int{}
	}
	c.gets[key]++
	return c.ObjectStore.Get(ctx, key)
}

func TestHydrateIdempotent(t *testing.T) {
	gitAvailable(t)
	mem := objectstore.NewMemStore()
	counted := &countingStore{ObjectStore: mem}
	cache := &RepoCache{Root: t.TempDir(), Engine: New(counted), Packs: NewBucketPackSource(counted)}
	seedFixtureRepo(t, mem, "t1", "acme", "widgets")
	ctx := context.Background()

	if _, err := cache.Hydrate(ctx, "t1", "acme", "widgets"); err != nil {
		t.Fatalf("first hydrate: %v", err)
	}
	afterFirst := map[string]int{}
	for k, v := range counted.gets {
		afterFirst[k] = v
	}

	refsFirst, err := os.ReadFile(filepath.Join(cache.Root, "t1", "acme", "widgets", "packed-refs"))
	if err != nil {
		t.Fatal(err)
	}

	dir2, err := cache.Hydrate(ctx, "t1", "acme", "widgets")
	if err != nil {
		t.Fatalf("second hydrate: %v", err)
	}
	// Repo dir is stable across hydrations (a warm cache).
	if dir2 != filepath.Join(cache.Root, "t1", "acme", "widgets") {
		t.Fatalf("hydrate dir moved: %s", dir2)
	}
	// No pack re-fetched: only the manifest is re-read (bucket truth).
	for k, v := range counted.gets {
		if strings.HasSuffix(k, ".pack") && v != afterFirst[k] {
			t.Fatalf("pack %s re-fetched: %d -> %d", k, afterFirst[k], v)
		}
	}

	// packed-refs is byte-identical across hydrations.
	refsSecond, err := os.ReadFile(filepath.Join(cache.Root, "t1", "acme", "widgets", "packed-refs"))
	if err != nil {
		t.Fatal(err)
	}
	if string(refsFirst) != string(refsSecond) {
		t.Fatalf("packed-refs changed across idempotent hydrations:\n%s\n---\n%s", refsFirst, refsSecond)
	}
}

func TestCloneOverSmartHTTP(t *testing.T) {
	for _, version := range []string{"0", "2"} {
		t.Run("protocol-v"+version, func(t *testing.T) {
			gitAvailable(t)
			mem, cache := newTestCache(t)
			m := seedFixtureRepo(t, mem, "t1", "acme", "widgets")

			h := &SmartHTTP{Hydrate: func(ctx context.Context, owner, repo string) (string, error) {
				return cache.Hydrate(ctx, "t1", owner, repo)
			}}
			srv := httptest.NewServer(http.StripPrefix("/git", http.Handler(h)))
			defer srv.Close()

			dst := t.TempDir()
			cmd := exec.Command("git", "-c", "protocol.version="+version, "clone", "-q", srv.URL+"/git/acme/widgets", dst)
			cmd.Env = append(os.Environ(),
				"GIT_AUTHOR_NAME=quill-test", "GIT_AUTHOR_EMAIL=quill-test@example.com",
				"GIT_COMMITTER_NAME=quill-test", "GIT_COMMITTER_EMAIL=quill-test@example.com")
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("git clone: %v\n%s", err, out)
			}

			// The cloned tree matches the fixture's content.
			for rel, want := range map[string]string{
				"README.md":    "# fixture\nhello from the fixture repo\n",
				"src/main.txt": "the answer is 42\n",
			} {
				got, err := os.ReadFile(filepath.Join(dst, rel))
				if err != nil {
					t.Fatalf("cloned %s missing: %v", rel, err)
				}
				if string(got) != want {
					t.Fatalf("cloned %s = %q, want %q", rel, got, want)
				}
			}
			// The tag made it through.
			out := gitCmd(t, dst, "describe", "--tags")
			if strings.TrimSpace(out) != "v1.0" {
				t.Fatalf("tag = %q", out)
			}
			// HEAD is at the manifest's recorded target for main.
			headSHA := strings.TrimSpace(gitCmd(t, dst, "rev-parse", "HEAD"))
			mainRef := m.Ref("refs/heads/main")
			if mainRef == nil || headSHA != mainRef.Target {
				t.Fatalf("HEAD = %s, manifest main = %+v", headSHA, mainRef)
			}
		})
	}
}

func TestSmartHTTPRequiresServiceParam(t *testing.T) {
	h := &SmartHTTP{Hydrate: func(ctx context.Context, owner, repo string) (string, error) {
		return "", fmt.Errorf("must not hydrate")
	}}
	srv := httptest.NewServer(http.StripPrefix("/git", http.Handler(h)))
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/git/acme/widgets/info/refs")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("GET /info/refs without service = %d, want 400", resp.StatusCode)
	}
}
