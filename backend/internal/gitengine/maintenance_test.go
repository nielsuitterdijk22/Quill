package gitengine

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nielsuitterdijk22/quill/internal/objectstore"
)

// Async maintenance (serverless epic, B7). Every test pins the two B7
// contracts: (1) idempotence — running twice leaves the same state and the
// second run reports no work; (2) safety — GC only runs after a cold-scratch
// verification proves the manifest serves the referee's refs.

func newMaintenance(t *testing.T) (*objectstore.MemStore, *Engine, *RepoCache, *memReferee, *memLeases, *Maintenance) {
	t.Helper()
	gitAvailable(t)
	mem := objectstore.NewMemStore()
	engine := New(mem)
	cache := &RepoCache{Root: t.TempDir(), Engine: engine, Packs: NewBucketPackSource(mem)}
	referee := newMemReferee(1, nil)
	leases := newMemLeases()
	mn := &Maintenance{Engine: engine, Cache: cache, Referee: referee, Leases: leases}
	return mem, engine, cache, referee, leases, mn
}

func maintCtx(t *testing.T) (context.Context, ReceiveContext) {
	t.Helper()
	return context.Background(), ReceiveContext{
		ID:  "11111111-1111-1111-1111-111111111111",
		Dir: t.TempDir(), TenantID: "11111111-2222-3333-4444-555555555555",
		ProjectSlug: "mnt-acme", RepoSlug: "fixture",
	}
}

// TestMaintenanceIdempotent proves the zero-work second run on a healthy
// repo: nothing reconciled, nothing deleted, byte-identical bucket.
func TestMaintenanceIdempotent(t *testing.T) {
	mem, _, _, referee, leases, mn := newMaintenance(t)
	ctx, rcx := maintCtx(t)
	m := seedFixtureRepo(t, mem, rcx.TenantID, rcx.ProjectSlug, rcx.RepoSlug)
	referee.refs = manifestRefs(m)
	before := strings.Join(mem.Keys(), ",")

	res, err := mn.Maintain(ctx, rcx, MaintenanceOptions{})
	if err != nil {
		t.Fatalf("first maintain: %v", err)
	}
	if res.ManifestRepaired || res.OrphanPacksDeleted > 0 || res.RepackedTo != 0 {
		t.Fatalf("first run should be a no-op on a healthy repo, got %+v", res)
	}
	if leases.called[rcx.ID] == 0 {
		t.Fatal("lease never acquired")
	}
	if leases.held[rcx.ID] {
		t.Fatal("lease leaked after release")
	}
	if got := strings.Join(mem.Keys(), ","); got != before {
		t.Fatalf("bucket changed:\n got %s\nwant %s", got, before)
	}

	// Second run: healthy repo, zero work again.
	res2, err := mn.Maintain(ctx, rcx, MaintenanceOptions{})
	if err != nil {
		t.Fatalf("second maintain: %v", err)
	}
	if res2.ManifestRepaired || res2.PacksAdopted > 0 || res2.OrphanPacksDeleted > 0 || res2.RepackedTo != 0 || res2.LeaseHeld {
		t.Fatalf("second run must be a no-op, got %+v", res2)
	}
}

// TestMaintenanceReconcilesLostManifestWrite simulates the B3 repair case: the
// referee committed a push (pack landed in the bucket) but the manifest
// write lost its etag race — the manifest regressed, so reads would serve
// nothing. Maintenance re-links the pack and re-materializes the manifest
// from the referee without moving bytes.
func TestMaintenanceReconcilesLostManifestWrite(t *testing.T) {
	mem, engine, cache, referee, _, mn := newMaintenance(t)
	ctx, rcx := maintCtx(t)
	m := seedFixtureRepo(t, mem, rcx.TenantID, rcx.ProjectSlug, rcx.RepoSlug)

	// The truth: the referee holds the seeded refs; the pack is in the bucket.
	referee.refs = manifestRefs(m)
	var packKey string
	for _, k := range mem.Keys() {
		if strings.HasSuffix(k, ".pack") {
			packKey = k
			break
		}
	}
	if packKey == "" {
		t.Fatal("fixture uploaded no pack")
	}

	// The lost write: the manifest regressed to an empty-view version (no
	// refs, no packs) — exactly the state a one-sided finalize failure
	// would leave behind on a repo born from this push.
	broken := NewManifest(rcx.TenantID, m.DefaultBranch)
	if _, err := engine.WriteManifest(ctx, rcx.TenantID, rcx.ProjectSlug, rcx.RepoSlug, broken, ""); err != nil {
		t.Fatal(err)
	}

	res, err := mn.Maintain(ctx, rcx, MaintenanceOptions{})
	if err != nil {
		t.Fatalf("maintain: %v", err)
	}
	if !res.ManifestRepaired || res.PacksAdopted != 1 {
		t.Fatalf("expected reconcile adopting 1 pack, got %+v", res)
	}
	fixed, _, err := engine.LoadManifest(ctx, rcx.TenantID, rcx.ProjectSlug, rcx.RepoSlug)
	if err != nil {
		t.Fatal(err)
	}
	if !refsEqual(referee.refs, manifestRefs(fixed)) {
		t.Fatalf("repaired refs = %v, want referee %v", fixed.Refs, referee.refs)
	}
	if len(fixed.Packs) != 1 {
		t.Fatalf("repaired manifest packs = %d, want 1", len(fixed.Packs))
	}
	// The re-linked view must actually serve: a cold clone of the repaired
	// state resolves the fixture's refs.
	dir, err := cache.Hydrate(ctx, rcx.TenantID, rcx.ProjectSlug, rcx.RepoSlug)
	if err != nil {
		t.Fatalf("hydrate repaired: %v", err)
	}
	gitCmd(t, dir, "rev-parse", "--verify", "refs/heads/main")
	gitCmd(t, dir, "fsck", "--connectivity-only")
}

// TestMaintenanceGarbageCollectsOrphans plants debris — a pack pair nothing
// references (crashed push leftover) — and proves it is deleted only after
// verification, while the referenced pack pair survives.
func TestMaintenanceGarbageCollectsOrphans(t *testing.T) {
	mem, engine, _, referee, _, mn := newMaintenance(t)
	ctx, rcx := maintCtx(t)
	m := seedFixtureRepo(t, mem, rcx.TenantID, rcx.ProjectSlug, rcx.RepoSlug)
	referee.refs = manifestRefs(m)

	// Plant an orphan pair: same bytes, unreferenced name.
	packBytes, _, err := mem.Get(ctx, PackKey(rcx.TenantID, rcx.ProjectSlug, rcx.RepoSlug, strings.TrimSuffix(filepath.Base(m.Packs[0].Object), ".pack"), "pack"))
	if err != nil {
		t.Fatal(err)
	}
	idxBytes, _, err := mem.Get(ctx, PackKey(rcx.TenantID, rcx.ProjectSlug, rcx.RepoSlug, strings.TrimSuffix(filepath.Base(m.Packs[0].Object), ".pack"), "idx"))
	if err != nil {
		t.Fatal(err)
	}
	orphanPack := PackKey(rcx.TenantID, rcx.ProjectSlug, rcx.RepoSlug, "deadbeef-orphan", "pack")
	orphanIdx := PackKey(rcx.TenantID, rcx.ProjectSlug, rcx.RepoSlug, "deadbeef-orphan", "idx")
	if _, err := mem.Put(ctx, orphanPack, packBytes, objectstore.PutOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := mem.Put(ctx, orphanIdx, idxBytes, objectstore.PutOptions{}); err != nil {
		t.Fatal(err)
	}

	res, err := mn.Maintain(ctx, rcx, MaintenanceOptions{})
	if err != nil {
		t.Fatalf("maintain: %v", err)
	}
	if res.ManifestRepaired {
		t.Fatal("healthy manifest must not repair")
	}
	if res.OrphanPacksDeleted != 1 {
		t.Fatalf("orphan pairs deleted = %d, want 1", res.OrphanPacksDeleted)
	}
	for _, k := range mem.Keys() {
		if strings.Contains(k, "deadbeef-orphan") {
			t.Fatalf("orphan survived GC: %s", k)
		}
	}
	// The live pair survives byte-for-byte.
	if _, _, err := engine.head(ctx, PackKey(rcx.TenantID, rcx.ProjectSlug, rcx.RepoSlug, strings.TrimSuffix(filepath.Base(m.Packs[0].Object), ".pack"), "pack")); err != nil {
		t.Fatalf("live pack vanished: %v", err)
	}
}

// gitStdin runs git in dir feeding stdin, failing the test on error.
// Identity env is pinned because commit-tree/mktree need an author identity
// and CI runners have no global git config.
func gitStdin(t *testing.T, dir, stdin string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com")
	cmd.Stdin = strings.NewReader(stdin)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// TestMaintenanceRepacks builds a genuinely incrementally-packed repo (the
// production shape: one pack per push) and proves consolidation: past the
// threshold, packs merge into one, the referee's storage re-stamps, the
// superseded pairs are GC'd, and the result still serves fsck — while a
// second run is a no-op.
func TestMaintenanceRepacks(t *testing.T) {
	mem, engine, cache, referee, _, mn := newMaintenance(t)
	ctx, rcx := maintCtx(t)
	m := seedFixtureRepo(t, mem, rcx.TenantID, rcx.ProjectSlug, rcx.RepoSlug)
	referee.refs = manifestRefs(m)

	// Materialize the fixture, add a second commit over its head, and pack
	// ONLY the delta (exactly what receive.unpackLimit=1 produces per push).
	dir, err := cache.Hydrate(ctx, rcx.TenantID, rcx.ProjectSlug, rcx.RepoSlug)
	if err != nil {
		t.Fatal(err)
	}
	blob := gitStdin(t, dir, "feature body\n", "hash-object", "-w", "--stdin")
	tree := gitStdin(t, dir, "100644 blob "+blob+"\tfeature.txt\n", "mktree")
	baseRef := strings.TrimSpace(gitCmd(t, dir, "rev-parse", "refs/heads/main"))
	commit := gitStdin(t, dir, "feature commit\n", "commit-tree", tree, "-p", baseRef, "-m", "second")
	gitCmd(t, dir, "update-ref", "refs/heads/feature", commit)
	// pack-objects takes the name base "…/pack" and writes pack-<hash>.pack
	// there, echoing just the hash.
	packBase := "pack-" + gitStdin(t, dir, "refs/heads/feature\n^refs/heads/main\n",
		"pack-objects", "--revs", filepath.Join(dir, "objects", "pack", "pack"))
	packPath := filepath.Join(dir, "objects", "pack", packBase+".pack")
	idxExpected := filepath.Join(dir, "objects", "pack", packBase+".idx")
	rec2, err := engine.UploadPack(ctx, rcx.TenantID, rcx.ProjectSlug, rcx.RepoSlug, packPath, idxExpected)
	if err != nil {
		t.Fatal(err)
	}

	m2 := *m
	m2.Packs = append(m2.Packs, rec2)
	m2.Refs = append(m2.Refs, ManifestRef{Name: "refs/heads/feature", Target: commit})
	m2.StorageBytes = m.StorageBytes + rec2.Bytes + rec2.IdxBytes
	if _, err := engine.WriteManifest(ctx, rcx.TenantID, rcx.ProjectSlug, rcx.RepoSlug, &m2, ""); err != nil {
		t.Fatal(err)
	}
	referee.refs = manifestRefs(&m2)

	res, err := mn.Maintain(ctx, rcx, MaintenanceOptions{RepackThreshold: 1})
	if err != nil {
		t.Fatalf("maintain+repack: %v", err)
	}
	if res.RepackedTo != 1 {
		t.Fatalf("expected consolidation to 1 pack, got %+v", res)
	}
	final, _, err := engine.LoadManifest(ctx, rcx.TenantID, rcx.ProjectSlug, rcx.RepoSlug)
	if err != nil {
		t.Fatal(err)
	}
	if len(final.Packs) != 1 {
		t.Fatalf("final pack count = %d, want 1", len(final.Packs))
	}
	if !refsEqual(referee.refs, manifestRefs(final)) {
		t.Fatal("repack lost refs")
	}
	st, err := referee.Load(ctx, rcx.ID)
	if err != nil {
		t.Fatal(err)
	}
	if st.StorageBytes != final.StorageBytes {
		t.Fatalf("referee storage %d != manifest %d", st.StorageBytes, final.StorageBytes)
	}
	// Exactly one pack pair remains for this repo.
	var packObjs int
	for _, k := range mem.Keys() {
		if strings.HasSuffix(k, ".pack") && strings.Contains(k, rcx.RepoSlug) {
			packObjs++
		}
	}
	if packObjs != 1 {
		t.Fatalf("bucket holds %d pack objects, want exactly 1", packObjs)
	}
	// The consolidated repo verifies from cold cache.
	scratch := &RepoCache{Root: t.TempDir(), Engine: engine, Packs: cache.Packs}
	cold, err := scratch.Hydrate(ctx, rcx.TenantID, rcx.ProjectSlug, rcx.RepoSlug)
	if err != nil {
		t.Fatalf("cold hydrate repacked: %v", err)
	}
	gitCmd(t, cold, "fsck", "--connectivity-only")
	gitCmd(t, cold, "rev-parse", "--verify", commit)

	// Idempotence: at the same threshold the second run repacks nothing
	// (1 pack is not above the threshold) and deletes nothing.
	res2, err := mn.Maintain(ctx, rcx, MaintenanceOptions{RepackThreshold: 1})
	if err != nil {
		t.Fatalf("second maintain: %v", err)
	}
	if res2.RepackedTo != 0 || res2.OrphanPacksDeleted > 0 || res2.ManifestRepaired {
		t.Fatalf("second run after repack should be a no-op, got %+v", res2)
	}
}

// TestMaintenanceRespectsLease proves a repo pushed right now is skipped, not
// fought over: the run reports lease-held and touches nothing.
func TestMaintenanceRespectsLease(t *testing.T) {
	mem, _, _, referee, leases, mn := newMaintenance(t)
	ctx, rcx := maintCtx(t)
	m := seedFixtureRepo(t, mem, rcx.TenantID, rcx.ProjectSlug, rcx.RepoSlug)
	referee.refs = manifestRefs(m)
	before := strings.Join(mem.Keys(), ",")

	release, err := leases.Acquire(ctx, rcx.ID, PushLeaseTTL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = release() }()

	res, err := mn.Maintain(ctx, rcx, MaintenanceOptions{})
	if err != nil {
		t.Fatalf("maintain under foreign lease: %v", err)
	}
	if !res.LeaseHeld || res.ManifestRepaired || res.OrphanPacksDeleted > 0 {
		t.Fatalf("expected clean skip, got %+v", res)
	}
	if got := strings.Join(mem.Keys(), ","); got != before {
		t.Fatal("bucket must be untouched when the lease is held")
	}
}

// TestMaintenanceRefusesGCWhenVerifyFails pins the safety interlock: a
// manifest whose packs cannot serve the referee's refs fails verification,
// and GC is skipped entirely rather than deleting debris around a broken
// view (reconcile-first can fail too; the caller sees the error and the
// bucket keeps everything).
func TestMaintenanceRefusesGCWhenVerifyFails(t *testing.T) {
	mem, engine, _, referee, _, mn := newMaintenance(t)
	ctx, rcx := maintCtx(t)

	// A manifest with refs pointing at objects that exist nowhere: the
	// referee and the manifest AGREE on refs (no repair triggers), so the
	// failure must come from verification — proving the GC interlock.
	m := NewManifest(rcx.TenantID, "main")
	m.Refs = []ManifestRef{{Name: "refs/heads/main", Target: "1111111111111111111111111111111111111111"}}
	if _, err := engine.WriteManifest(ctx, rcx.TenantID, rcx.ProjectSlug, rcx.RepoSlug, m, ""); err != nil {
		t.Fatal(err)
	}
	referee.refs = manifestRefs(m)

	_, err := mn.Maintain(ctx, rcx, MaintenanceOptions{})
	if err == nil {
		t.Fatal("expected verification failure for refs without objects")
	}
	if _, _, gerr := mem.Get(ctx, ManifestKey(rcx.TenantID, rcx.ProjectSlug, rcx.RepoSlug)); gerr != nil {
		t.Fatal("manifest must survive a failed maintenance run")
	}
}
