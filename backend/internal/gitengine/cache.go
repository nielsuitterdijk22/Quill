package gitengine

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// RepoCache is the disposable-cache half of "bucket is the repository":
// a per-instance working tree of hydrated bare repos. The cache root is
// RAM-backed in Serverless Containers (writable layer counts against
// memory_limit) and a real disk in warm pools later — either way the bucket
// stays the only truth and hydration must be safe to redo at any time.
type RepoCache struct {
	// Root is the directory hydrated repos live under.
	Root string
	// Engine loads manifests (bucket truth).
	Engine *Engine
	// Packs materializes pack objects into the hydrated repo.
	Packs PackSource
}

// Hydrate ensures a hydrated bare repo for one repository exists and matches
// the bucket, returning its directory. Idempotent and re-entrant: the
// manifest is re-read every call, refs are re-materialized from it, and packs
// are only fetched when absent (size-matched). B3's write path may bump
// revision while we run — hydration is read-only and races with a push by
// ending up with either revision's packs on disk; the next call re-syncs.
func (c *RepoCache) Hydrate(ctx context.Context, tenantID, projectSlug, repoSlug string) (string, error) {
	m, _, err := c.Engine.LoadManifest(ctx, tenantID, projectSlug, repoSlug)
	if err != nil {
		return "", err
	}
	dir := filepath.Join(c.Root, tenantID, projectSlug, repoSlug)
	if err := ensureBareRepo(dir); err != nil {
		return "", err
	}
	if err := materializeRefs(dir, m); err != nil {
		return "", err
	}
	prefix := RepoPrefix(tenantID, projectSlug, repoSlug)
	for _, p := range m.Packs {
		if _, _, err := c.Packs.FetchPack(ctx, prefix, p, filepath.Join(dir, "objects", "pack")); err != nil {
			return "", err
		}
	}
	return dir, nil
}

// ensureBareRepo creates the git skeleton when the directory is fresh.
func ensureBareRepo(dir string) error {
	if _, err := os.Stat(filepath.Join(dir, "HEAD")); err == nil {
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	cmd := exec.Command("git", "init", "--bare", "--quiet", dir)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git init --bare %s: %w: %s", dir, err, out)
	}
	return nil
}

// materializeRefs writes HEAD + packed-refs from the manifest (the referee's
// view). Written on EVERY hydration: a stale cache can never serve refs the
// referee has moved.
func materializeRefs(dir string, m *Manifest) error {
	if err := os.WriteFile(filepath.Join(dir, "HEAD"), []byte("ref: refs/heads/"+m.DefaultBranch+"\n"), 0o644); err != nil {
		return err
	}
	refs := make([]ManifestRef, len(m.Refs))
	copy(refs, m.Refs)
	sort.Slice(refs, func(i, j int) bool { return refs[i].Name < refs[j].Name })
	var b strings.Builder
	b.WriteString("# pack-refs with: peeled fully-peeled sorted\n")
	for _, r := range refs {
		fmt.Fprintf(&b, "%s %s\n", r.Target, r.Name)
		if r.Peeled != "" {
			fmt.Fprintf(&b, "^%s\n", r.Peeled)
		}
	}
	return writeAtomic(filepath.Join(dir, "packed-refs"), []byte(b.String()))
}
