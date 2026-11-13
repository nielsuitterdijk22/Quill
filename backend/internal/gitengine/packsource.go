package gitengine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// PackSource materializes a manifest's packs for the read path. The git
// engine consumes packs ONLY through this seam (roadmap's streaming rule):
// today's implementation downloads whole objects into the hydration
// directory; a later range-request implementation can serve partial content
// without changing any caller.
type PackSource interface {
	// FetchPack downloads one pack and its sidecar idx into dir and returns
	// their file paths. repoPrefix is the repo's bucket key prefix
	// (RepoPrefix); the manifest records pack objects repo-relative
	// ("packs/<sha>.pack") so manifests stay layout-portable. Idempotent: a
	// file whose size already matches the manifest's record is reused, not
	// re-fetched.
	FetchPack(ctx context.Context, repoPrefix string, p ManifestPack, dir string) (packPath, idxPath string, err error)
}

// BucketPackSource is the full-download PackSource over an ObjectStore.
type BucketPackSource struct {
	store ObjectStore
}

// NewBucketPackSource builds the default pack source over store.
func NewBucketPackSource(store ObjectStore) *BucketPackSource {
	return &BucketPackSource{store: store}
}

// FetchPack implements PackSource.
func (b *BucketPackSource) FetchPack(ctx context.Context, repoPrefix string, p ManifestPack, dir string) (string, string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", "", fmt.Errorf("fetch pack: %w", err)
	}
	packPath := filepath.Join(dir, filepath.Base(p.Object))
	if err := b.fetchObject(ctx, repoPrefix+p.Object, p.Bytes, packPath); err != nil {
		return "", "", err
	}
	idxPath := ""
	if p.IdxObject != "" {
		idxPath = filepath.Join(dir, filepath.Base(p.IdxObject))
		if err := b.fetchObject(ctx, repoPrefix+p.IdxObject, p.IdxBytes, idxPath); err != nil {
			return "", "", err
		}
	}
	return packPath, idxPath, nil
}

// fetchObject downloads key into dest unless dest already exists with the
// recorded size (hydration idempotence).
func (b *BucketPackSource) fetchObject(ctx context.Context, key string, wantBytes int64, dest string) error {
	if st, err := os.Stat(dest); err == nil && wantBytes > 0 && st.Size() == wantBytes {
		return nil
	}
	body, _, err := b.store.Get(ctx, key)
	if err != nil {
		return fmt.Errorf("fetch %s: %w", key, err)
	}
	if wantBytes > 0 && int64(len(body)) != wantBytes {
		return fmt.Errorf("fetch %s: %d bytes, manifest says %d — manifest and bucket disagree", key, len(body), wantBytes)
	}
	return writeAtomic(dest, body)
}

// writeAtomic writes dest via a temp file + rename so a crashed hydration
// never leaves a half-written pack behind (the idempotence check only trusts
// complete files of the right size).
//
// Each writer stages in its OWN temp file: concurrent hydrations of one repo
// (post-receive CI trigger vs the next request) used to share dest+".part",
// so one writer renamed the other's staging file away → "no such file" 500s.
// rename(2) is atomic, so the last writer wins with a complete file.
func writeAtomic(dest string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(dest), filepath.Base(dest)+".part-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Chmod(tmp, 0o644); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, dest); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}
