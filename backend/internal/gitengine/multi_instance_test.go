package gitengine

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/nielsuitterdijk22/quill/internal/objectstore"
)

// Two api instances (max_scale 2) share the bucket + referee but each has its
// own hydration cache. receive-pack writes LOOSE refs, and loose refs shadow
// the packed-refs view Hydrate materializes from the manifest — so instance
// A's leftover loose `main` from an earlier push would be snapshotted as the
// truth on A's NEXT push, after instance B had moved main (reverting B's push
// or wedging A). Every push must leave the cache with no stale loose refs.
func TestPushAcrossInstancesKeepsOtherInstancesRefs(t *testing.T) {
	gitAvailable(t)
	store := objectstore.NewMemStore()
	referee := newMemReferee(1, nil)
	a := newReceiveFixture(t, store, referee, nil, nil)
	b := newReceiveFixture(t, store, referee, nil, nil)
	ctx := context.Background()
	if err := a.engine.ProvisionRepo(ctx, "t1", "acme", "multi", "main"); err != nil {
		t.Fatalf("provision: %v", err)
	}
	urlA := a.server.URL + "/git/acme/multi"
	urlB := b.server.URL + "/git/acme/multi"

	clone := func(url string) string {
		t.Helper()
		dir := t.TempDir()
		cmd := exec.Command("git", "clone", "-q", url, dir)
		cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("clone %s: %v\n%s", url, err, out)
		}
		return dir
	}

	// 1. main=X through instance A (A's cache now holds a loose refs/heads/main).
	w := clone(urlA)
	writeFixtureCommit(t, w, "x", "x.txt", "x\n")
	gitCmd(t, w, "commit", "-qm", "X")
	if out, err := gitPush(t, w, "origin", "main"); err != nil {
		t.Fatalf("push X via A: %v\n%s", err, out)
	}

	// 2. main=Y through instance B.
	wb := clone(urlB)
	writeFixtureCommit(t, wb, "y", "y.txt", "y\n")
	gitCmd(t, wb, "commit", "-qm", "Y")
	y := strings.TrimSpace(gitCmd(t, wb, "rev-parse", "HEAD"))
	if out, err := gitPush(t, wb, "origin", "main"); err != nil {
		t.Fatalf("push Y via B: %v\n%s", err, out)
	}

	// 3. A serves main=Y to readers...
	r := clone(urlA)
	if got := strings.TrimSpace(gitCmd(t, r, "rev-parse", "HEAD")); got != y {
		t.Fatalf("instance A serves main=%s after B pushed %s (stale loose ref)", got, y)
	}

	// 4. ...and a push of an unrelated branch through A keeps main=Y.
	gitCmd(t, r, "checkout", "-qb", "feature")
	writeFixtureCommit(t, r, "f", "f.txt", "f\n")
	gitCmd(t, r, "commit", "-qm", "F")
	if out, err := gitPush(t, r, "origin", "feature"); err != nil {
		t.Fatalf("push feature via A: %v\n%s", err, out)
	}
	m, _, err := a.engine.LoadManifest(ctx, "t1", "acme", "multi")
	if err != nil {
		t.Fatal(err)
	}
	if ref := m.Ref("refs/heads/main"); ref == nil || ref.Target != y {
		t.Fatalf("manifest main = %+v after A's feature push, want %s (B's push reverted)", ref, y)
	}
	if m.Ref("refs/heads/feature") == nil {
		t.Fatal("feature push lost")
	}
}
