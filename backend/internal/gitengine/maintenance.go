package gitengine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/nielsuitterdijk22/quill/internal/objectstore"
)

// Async maintenance (serverless epic, PR B7). Maintenance is WRITER work on a
// repo: it takes the same push lease the receive path takes, runs entirely
// outside request paths, and derives all of its work from the two sources of
// truth (referee row + bucket listing) — so any run is idempotent and safe to
// repeat, kill, or run concurrently with reads. It never runs inline in a
// request: a request path that can wedge on big repos is the VM-wedge failure
// mode this epic exists to eliminate.

// ListSource is the optional object-store enumeration maintenance needs; both
// objectstore clients implement it. The engine's request paths never list.
type ListSource interface {
	List(ctx context.Context, prefix string) ([]string, error)
}

// ErrListingUnsupported means the configured object store cannot enumerate
// (engine built over a minimal test double without List).
var ErrListingUnsupported = errors.New("object store does not support listing")

// ListPacks enumerates a repo's bucket prefix and returns the base names of
// every packfile pair present (pack-<sha>.pack/.idx collapse to <sha>).
func (e *Engine) ListPacks(ctx context.Context, tenantID, projectSlug, repoSlug string) ([]string, error) {
	ls, ok := e.store.(ListSource)
	if !ok {
		return nil, ErrListingUnsupported
	}
	keys, err := ls.List(ctx, RepoPrefix(tenantID, projectSlug, repoSlug)+"packs/")
	if err != nil {
		return nil, err
	}
	names := map[string]bool{}
	for _, k := range keys {
		base := filepath.Base(k)
		switch {
		case strings.HasSuffix(base, ".pack"):
			names[strings.TrimSuffix(base, ".pack")] = true
		case strings.HasSuffix(base, ".idx"):
			names[strings.TrimSuffix(base, ".idx")] = true
		}
	}
	out := make([]string, 0, len(names))
	for n := range names {
		out = append(out, n)
	}
	sort.Strings(out)
	return out, nil
}

// Maintenance runs the B7 jobs over one repo at a time. Engine and Referee
// are required; Cache is required for repack/verify (it carries the PackSource).
type Maintenance struct {
	Engine  *Engine
	Cache   *RepoCache
	Referee Referee
	Leases  PushLeases
}

// MaintenanceOptions toggles the optional work. Zero value = reconcile + GC
// only, which is idempotent and cheap.
type MaintenanceOptions struct {
	// RepackThreshold consolidates packs into one when the manifest lists
	// MORE than this many packs (0 = never).
	RepackThreshold int
}

// MaintenanceResult reports what one run did (idempotence proof: a second
// run on a healthy repo reports all-zero work).
type MaintenanceResult struct {
	LeaseHeld          bool
	ManifestRepaired   bool
	PacksAdopted       int // packs found in the bucket and re-linked into the manifest
	RepackedTo         int // pack count after repack (0 = repack not run)
	OrphanPacksDeleted int
}

// Maintain repairs a repo's manifest against the referee, repacks past the
// threshold, then garbage-collects unreferenced packs. Order matters:
// reconcile first (repair the view), repack second (rewrite packs under the
// repaired view), verify third (prove the manifest serves the referee's refs),
// GC last (only delete what the verified manifest does not reference).
func (mn *Maintenance) Maintain(ctx context.Context, rcx ReceiveContext, opts MaintenanceOptions) (*MaintenanceResult, error) {
	res := &MaintenanceResult{}

	release, err := mn.Leases.Acquire(ctx, rcx.ID, PushLeaseTTL)
	if err != nil {
		if errors.Is(err, ErrLeaseHeld) {
			res.LeaseHeld = true
			return res, nil
		}
		return res, fmt.Errorf("acquire lease: %w", err)
	}
	defer func() { _ = release() }()

	m, _, err := mn.Engine.LoadManifest(ctx, rcx.TenantID, rcx.ProjectSlug, rcx.RepoSlug)
	if err != nil {
		return res, err
	}
	st, err := mn.Referee.Load(ctx, rcx.ID)
	if err != nil {
		return res, err
	}

	listed, err := mn.Engine.ListPacks(ctx, rcx.TenantID, rcx.ProjectSlug, rcx.RepoSlug)
	if err != nil {
		return res, fmt.Errorf("list packs: %w", err)
	}

	// ---- reconcile: the referee is the truth, the manifest a repairable
	// view. A finalized push whose manifest write lost the etag race leaves
	// refs/packs behind that only a listing can re-link.
	manifestPacks := map[string]bool{}
	for _, p := range m.Packs {
		manifestPacks[strings.TrimSuffix(filepath.Base(p.Object), ".pack")] = true
	}
	var extras []string
	for _, n := range listed {
		if !manifestPacks[n] {
			extras = append(extras, n)
		}
	}
	// Adoption rule: extras re-link into the manifest ONLY when the view
	// actually regressed (refs diverged) — the lost-manifest-write repair.
	// When refs match, the manifest already serves the referee's refs and
	// extras are debris for the GC step; hoarding them would pin garbage
	// into the view forever.
	if !refsEqual(st.Refs, manifestRefs(m)) {
		if err := mn.reconcile(ctx, rcx, m, st, extras); err != nil {
			return res, err
		}
		res.ManifestRepaired = true
		res.PacksAdopted = len(extras)
		if m, _, err = mn.Engine.LoadManifest(ctx, rcx.TenantID, rcx.ProjectSlug, rcx.RepoSlug); err != nil {
			return res, err
		}
	}

	// ---- repack: one packfile instead of N (cheaper cold starts, smaller
	// hydration). Same lease justification as a push; the referee CAS
	// re-stamps storage (revision safely advances — refs are unchanged).
	if opts.RepackThreshold > 0 && len(m.Packs) > opts.RepackThreshold {
		packs, err := mn.repack(ctx, rcx, m, st)
		if err != nil {
			return res, err
		}
		res.RepackedTo = len(packs)
		if m, _, err = mn.Engine.LoadManifest(ctx, rcx.TenantID, rcx.ProjectSlug, rcx.RepoSlug); err != nil {
			return res, err
		}
		st, err = mn.Referee.Load(ctx, rcx.ID)
		if err != nil {
			return res, err
		}
	}

	// ---- verify: prove the manifest we are about to GC around serves the
	// referee's refs from scratch — then deletion cannot bite a reader.
	if err := mn.verify(ctx, rcx); err != nil {
		return res, fmt.Errorf("verification failed; skipped GC: %w", err)
	}

	// ---- GC: anything in the bucket the manifest does not reference is
	// debris from crashed pushes or lost races (or pre-repack superseded
	// packs); delete it. Missing objects (double run) are fine.
	finals := map[string]bool{}
	for _, p := range m.Packs {
		finals[strings.TrimSuffix(filepath.Base(p.Object), ".pack")] = true
	}
	for _, n := range listed {
		if finals[n] {
			continue
		}
		for _, kind := range []string{"pack", "idx"} {
			if err := mn.Engine.delete(ctx, PackKey(rcx.TenantID, rcx.ProjectSlug, rcx.RepoSlug, n, kind)); err != nil && !errors.Is(err, objectstore.ErrNotFound) {
				return res, err
			}
		}
		res.OrphanPacksDeleted++
	}
	return res, nil
}

// reconcile rewrites the manifest as the referee's view: refs from the row,
// packs = manifest packs + every extra pair found in the bucket. The write is
// unconditional — the lease makes this the only manifest writer, the same
// justification the push finalize path uses for its last attempt.
func (mn *Maintenance) reconcile(ctx context.Context, rcx ReceiveContext, m *Manifest, st RefereeState, extras []string) error {
	m2 := *m
	m2.Refs = refsFromMap(st.Refs)
	packs := make([]ManifestPack, len(m.Packs), len(m.Packs)+len(extras))
	copy(packs, m.Packs)
	var storage int64
	for _, p := range packs {
		storage += p.Bytes + p.IdxBytes
	}
	for _, n := range extras {
		rec, err := mn.packRecord(ctx, rcx, n)
		if err != nil {
			return err
		}
		packs = append(packs, rec)
		storage += rec.Bytes + rec.IdxBytes
	}
	m2.Packs = packs
	m2.StorageBytes = storage
	m2.UpdatedAt = time.Now().UTC()
	if _, err := mn.Engine.WriteManifest(ctx, rcx.TenantID, rcx.ProjectSlug, rcx.RepoSlug, &m2, ""); err != nil {
		return fmt.Errorf("repair manifest: %w", err)
	}
	return nil
}

// packRecord builds the manifest entry for a pack pair already in the bucket
// (sizes + etags via Head; no bytes move).
func (mn *Maintenance) packRecord(ctx context.Context, rcx ReceiveContext, base string) (ManifestPack, error) {
	packBytes, packEtag, err := mn.Engine.head(ctx, PackKey(rcx.TenantID, rcx.ProjectSlug, rcx.RepoSlug, base, "pack"))
	if err != nil {
		return ManifestPack{}, err
	}
	rec := ManifestPack{Object: "packs/" + base + ".pack", Bytes: packBytes, ETag: packEtag}
	if idxBytes, idxEtag, err := mn.Engine.head(ctx, PackKey(rcx.TenantID, rcx.ProjectSlug, rcx.RepoSlug, base, "idx")); err == nil {
		rec.IdxObject = "packs/" + base + ".idx"
		rec.IdxBytes = idxBytes
		rec.IdxETag = idxEtag
	}
	return rec, nil
}

// repack consolidates all packs into one inside a scratch hydration, uploads
// it, and swaps the manifest. `git repack -a -d` packs the reachable set —
// unreachable objects (history rewritten away) drop out, which is the
// repository contract: the manifest is the reachable view.
func (mn *Maintenance) repack(ctx context.Context, rcx ReceiveContext, m *Manifest, st RefereeState) ([]ManifestPack, error) {
	if mn.Cache == nil {
		return nil, fmt.Errorf("repack requires a cache (scratch hydration)")
	}
	scratch := &RepoCache{Root: filepath.Join(os.TempDir(), "quill-mnt-"+rcx.ID), Engine: mn.Engine, Packs: mn.Cache.Packs}
	defer func() { _ = os.RemoveAll(scratch.Root) }()
	dir, err := scratch.Hydrate(ctx, rcx.TenantID, rcx.ProjectSlug, rcx.RepoSlug)
	if err != nil {
		return nil, fmt.Errorf("scratch hydrate: %w", err)
	}
	cmd := exec.CommandContext(ctx, "git", "repack", "-a", "-d", "--quiet")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("git repack: %w: %s", err, out)
	}
	packDir := filepath.Join(dir, "objects", "pack")
	entries, err := os.ReadDir(packDir)
	if err != nil {
		return nil, err
	}
	var packs []ManifestPack
	var storage int64
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".pack") {
			continue
		}
		base := strings.TrimSuffix(e.Name(), ".pack")
		rec, err := mn.Engine.UploadPack(ctx, rcx.TenantID, rcx.ProjectSlug, rcx.RepoSlug, filepath.Join(packDir, base+".pack"), filepath.Join(packDir, base+".idx"))
		if err != nil {
			return nil, err
		}
		packs = append(packs, rec)
		storage += rec.Bytes + rec.IdxBytes
	}
	m2 := *m
	m2.Packs = packs
	m2.StorageBytes = storage
	m2.UpdatedAt = time.Now().UTC()
	if _, err := mn.Referee.Commit(ctx, rcx.ID, st, manifestRefs(&m2), storage); err != nil {
		return nil, fmt.Errorf("re-stamp storage in referee: %w", err)
	}
	if _, err := mn.Engine.WriteManifest(ctx, rcx.TenantID, rcx.ProjectSlug, rcx.RepoSlug, &m2, ""); err != nil {
		return nil, fmt.Errorf("swap repacked manifest: %w", err)
	}
	return packs, nil
}

// verify does a cold hydration into a scratch dir and runs git fsck: every
// referee ref must resolve fully from the manifest's packs alone. GC after a
// green verify cannot delete anything a reader needs.
func (mn *Maintenance) verify(ctx context.Context, rcx ReceiveContext) error {
	if mn.Cache == nil {
		return fmt.Errorf("verify requires a cache (scratch hydration)")
	}
	scratch := &RepoCache{Root: filepath.Join(os.TempDir(), "quill-verify-"+rcx.ID), Engine: mn.Engine, Packs: mn.Cache.Packs}
	defer func() { _ = os.RemoveAll(scratch.Root) }()
	dir, err := scratch.Hydrate(ctx, rcx.TenantID, rcx.ProjectSlug, rcx.RepoSlug)
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, "git", "fsck", "--connectivity-only", "--no-progress")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git fsck: %w: %s", err, out)
	}
	return nil
}

// head and delete expose the engine's object store to this file without
// widening the public surface.
func (e *Engine) head(ctx context.Context, key string) (int64, string, error) {
	return e.store.Head(ctx, key)
}
func (e *Engine) delete(ctx context.Context, key string) error { return e.store.Delete(ctx, key) }

// refsFromMap sorts a referee refs map into manifest refs (peeled unknown —
// reconciliation is refs-only; git peels lazily without the packed-refs hint).
func refsFromMap(refs map[string]string) []ManifestRef {
	out := make([]ManifestRef, 0, len(refs))
	for name, target := range refs {
		out = append(out, ManifestRef{Name: name, Target: target})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// DeleteRepoObjects removes every object under a repo's bucket prefix (packs,
// manifest, anything else) — repository deletion. Requires a listing store.
// Missing objects are fine: a retried delete converges.
func (e *Engine) DeleteRepoObjects(ctx context.Context, tenantID, projectSlug, repoSlug string) (int, error) {
	ls, ok := e.store.(ListSource)
	if !ok {
		return 0, ErrListingUnsupported
	}
	keys, err := ls.List(ctx, RepoPrefix(tenantID, projectSlug, repoSlug))
	if err != nil {
		return 0, err
	}
	n := 0
	for _, k := range keys {
		if err := e.store.Delete(ctx, k); err != nil && !errors.Is(err, objectstore.ErrNotFound) {
			return n, fmt.Errorf("delete %s: %w", k, err)
		}
		n++
	}
	return n, nil
}

// CopyRepoObjects copies every object of one repo to another repo prefix (a
// project or repo slug rename: keys are slug-addressed, manifests hold only
// repo-relative pack paths). The manifest is written last, so the target is
// never readable before its packs exist. Existing target objects are
// overwritten. Returns how many objects were copied.
func (e *Engine) CopyRepoObjects(ctx context.Context, tenantID, fromProject, fromRepo, toProject, toRepo string) (int, error) {
	ls, ok := e.store.(ListSource)
	if !ok {
		return 0, ErrListingUnsupported
	}
	from := RepoPrefix(tenantID, fromProject, fromRepo)
	to := RepoPrefix(tenantID, toProject, toRepo)
	if from == to {
		return 0, nil
	}
	keys, err := ls.List(ctx, from)
	if err != nil {
		return 0, err
	}
	manifest := ManifestKey(tenantID, fromProject, fromRepo)
	sort.SliceStable(keys, func(i, j int) bool { return keys[j] == manifest && keys[i] != manifest })
	n := 0
	for _, k := range keys {
		body, _, err := e.store.Get(ctx, k)
		if err != nil {
			return n, fmt.Errorf("read %s: %w", k, err)
		}
		if _, err := e.store.Put(ctx, to+strings.TrimPrefix(k, from), body, objectstore.PutOptions{}); err != nil {
			return n, fmt.Errorf("write %s: %w", to+strings.TrimPrefix(k, from), err)
		}
		n++
	}
	return n, nil
}
