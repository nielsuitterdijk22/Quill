package platform_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nielsuitterdijk22/quill/internal/platform"
)

// fakeSBOM records the scanned tree and returns a fixed CycloneDX document.
type fakeSBOM struct {
	calls   atomic.Int64
	sawFile atomic.Bool
	fail    bool
}

func (f *fakeSBOM) Generate(_ context.Context, dir string) ([]byte, error) {
	f.calls.Add(1)
	if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
		f.sawFile.Store(true)
	}
	if f.fail {
		return nil, errors.New("syft exploded")
	}
	return []byte(`{"bomFormat":"CycloneDX","specVersion":"1.6","components":[{"name":"a"},{"name":"b"},{"name":"c"}]}`), nil
}

func sbomRepo(t *testing.T, slug string, gen platform.SBOMGenerator) (*platform.Service, platform.Actor, string, uuid.UUID, string) {
	t.Helper()
	browseGitAvailable(t)
	svc, st, gitURL := browseFixture(t)
	svc = svc.WithSBOMGenerator(gen)
	ctx := context.Background()
	act := actor(makeUser(t, st, slug+"-owner"))
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
	os.WriteFile(filepath.Join(work, "go.mod"), []byte("module example.com/app\n\ngo 1.22\n"), 0o644)
	browseGit(t, work, "add", "-A")
	browseGit(t, work, "commit", "-qm", "init")
	head := browseGit(t, work, "rev-parse", "HEAD")
	if out, err := browseGitPush(t, work, "origin", "main"); err != nil {
		t.Fatalf("push: %v\n%s", err, out)
	}
	return svc, act, slug, repo.ID, head
}

func waitSBOM(t *testing.T, svc *platform.Service, act platform.Actor, slug, want string) *platform.SBOMView {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		v, err := svc.RepoSBOM(context.Background(), act, slug, "app")
		if err != nil {
			t.Fatalf("RepoSBOM: %v", err)
		}
		if v != nil && v.Status == want {
			return v
		}
		time.Sleep(100 * time.Millisecond)
	}
	v, _ := svc.RepoSBOM(context.Background(), act, slug, "app")
	t.Fatalf("SBOM never reached %q: %+v", want, v)
	return nil
}

func TestSBOMOnDefaultBranchPush(t *testing.T) {
	gen := &fakeSBOM{}
	svc, act, slug, repoID, head := sbomRepo(t, "sbom-a", gen)
	ctx := context.Background()

	// Nothing yet.
	if v, err := svc.RepoSBOM(ctx, act, slug, "app"); err != nil || v != nil {
		t.Fatalf("before push: %+v %v", v, err)
	}
	// A feature-branch push never makes an SBOM.
	if _, err := svc.HandleEnginePush(ctx, repoID, []platform.EngineRefUpdate{{Ref: "refs/heads/feature", New: head}}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
	if gen.calls.Load() != 0 {
		t.Fatal("SBOM generated for a non-default branch")
	}

	// The default-branch push does.
	if _, err := svc.HandleEnginePush(ctx, repoID, []platform.EngineRefUpdate{{Ref: "refs/heads/main", New: head}}); err != nil {
		t.Fatal(err)
	}
	v := waitSBOM(t, svc, act, slug, "ready")
	if v.CommitSHA != head || v.ComponentCount != 3 || v.SpecVersion != "1.6" || v.Format != "cyclonedx-json" {
		t.Fatalf("view = %+v", v)
	}
	if !gen.sawFile.Load() {
		t.Fatal("generator did not see the commit's tree (go.mod)")
	}
	doc, name, err := svc.RepoSBOMDocument(ctx, act, slug, "app", "")
	if err != nil {
		t.Fatalf("document: %v", err)
	}
	if !strings.Contains(string(doc), "CycloneDX") || !strings.HasPrefix(name, "app-"+head[:7]) {
		t.Fatalf("doc %q name %q", doc, name)
	}

	// The same commit pushed again is not regenerated.
	if _, err := svc.HandleEnginePush(ctx, repoID, []platform.EngineRefUpdate{{Ref: "refs/heads/main", New: head}}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
	if n := gen.calls.Load(); n != 1 {
		t.Fatalf("generator calls = %d, want 1", n)
	}

	// Non-members of a private repo see nothing.
	_, st := newService(t)
	stranger := actor(makeUser(t, st, "sbom-stranger"))
	if _, err := svc.RepoSBOM(ctx, stranger, slug, "app"); err == nil {
		t.Fatal("stranger read a private repo's SBOM")
	}
	if _, _, err := svc.RepoSBOMDocument(ctx, stranger, slug, "app", ""); err == nil {
		t.Fatal("stranger downloaded a private repo's SBOM")
	}
}

func TestSBOMFailureAndRegenerate(t *testing.T) {
	gen := &fakeSBOM{fail: true}
	svc, act, slug, _, head := sbomRepo(t, "sbom-b", gen)
	ctx := context.Background()

	// Explicit request (repos migrated before the feature have no SBOM yet).
	if _, err := svc.RequestRepoSBOM(ctx, act, slug, "app"); err != nil {
		t.Fatalf("RequestRepoSBOM: %v", err)
	}
	v := waitSBOM(t, svc, act, slug, "failed")
	if v.CommitSHA != head || !strings.Contains(v.Error, "syft exploded") {
		t.Fatalf("failed view = %+v", v)
	}
	if _, _, err := svc.RepoSBOMDocument(ctx, act, slug, "app", ""); err == nil {
		t.Fatal("download served without a ready SBOM")
	}

	// Retry after the cause is fixed.
	gen.fail = false
	if _, err := svc.RequestRepoSBOM(ctx, act, slug, "app"); err != nil {
		t.Fatalf("regenerate: %v", err)
	}
	waitSBOM(t, svc, act, slug, "ready")
}

// SBOMs are always there (owner, 2026-11-14): the landing page read makes
// sure the default branch head has one even when no push hook ever fired
// (repos older than the feature), and the maintenance backfill covers repos
// nobody has opened.
func TestSBOMEnsuredWithoutPushHook(t *testing.T) {
	gen := &fakeSBOM{}
	svc, act, slug, _, head := sbomRepo(t, "sbom-ensure", gen)
	ctx := context.Background()
	st, err := svc.EnsureRepoSBOM(ctx, act, slug, "app")
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if st.Current == nil || st.Current.CommitSHA != head {
		t.Fatalf("ensure did not queue the head SBOM: %+v", st.Current)
	}
	v := waitSBOM(t, svc, act, slug, "ready")
	if v.CommitSHA != head || v.ComponentCount != 3 {
		t.Fatalf("view = %+v", v)
	}
	// Reading again never regenerates.
	if _, err := svc.EnsureRepoSBOM(ctx, act, slug, "app"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
	if n := gen.calls.Load(); n != 1 {
		t.Fatalf("generator calls = %d, want 1", n)
	}
}

func TestSBOMBackfill(t *testing.T) {
	gen := &fakeSBOM{}
	svc, act, slug, _, head := sbomRepo(t, "sbom-backfill", gen)
	ctx := context.Background()
	generated, _, err := svc.BackfillSBOMs(ctx)
	if err != nil {
		t.Fatalf("backfill: %v", err)
	}
	if generated < 1 {
		t.Fatalf("backfill generated %d", generated)
	}
	v, err := svc.RepoSBOM(ctx, act, slug, "app")
	if err != nil || v == nil || v.Status != "ready" || v.CommitSHA != head {
		t.Fatalf("after backfill: %+v %v", v, err)
	}
	// A second pass is a no-op for this repo.
	before := gen.calls.Load()
	if _, _, err := svc.BackfillSBOMs(ctx); err != nil {
		t.Fatal(err)
	}
	if gen.calls.Load() != before {
		// Other test repos in the shared DB may still lack SBOMs; this repo
		// must not be regenerated.
		if v2, _ := svc.RepoSBOM(ctx, act, slug, "app"); v2 == nil || v2.CommitSHA != head || v2.Status != "ready" {
			t.Fatalf("second backfill disturbed the repo: %+v", v2)
		}
	}
}
