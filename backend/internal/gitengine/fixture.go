package gitengine

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/nielsuitterdijk22/quill/internal/objectstore"
)

// SeedFixture builds a real content-bearing repository (two commits with
// files, one annotated tag) in the empty work dir src, uploads its single
// pack + idx + manifest to store under the repo's key prefix, and returns the
// manifest. It is the bucket-side shape pushes produce (B3) and migrations
// will produce (Phase D): integration tests across packages use it to stand
// up a browsable/cloneable repo without going through HTTP.
func SeedFixture(ctx context.Context, store ObjectStore, src, tenantID, projectSlug, repoSlug string) (*Manifest, error) {
	run := func(dir string, args ...string) (string, error) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=quill-test", "GIT_AUTHOR_EMAIL=quill-test@example.com",
			"GIT_COMMITTER_NAME=quill-test", "GIT_COMMITTER_EMAIL=quill-test@example.com")
		out, err := cmd.CombinedOutput()
		if err != nil {
			return "", fmt.Errorf("git %v: %w: %s", args, err, out)
		}
		return string(out), nil
	}
	mustRun := func(dir string, args ...string) error {
		if _, err := run(dir, args...); err != nil {
			return err
		}
		return nil
	}
	write := func(rel, content string) error {
		dest := filepath.Join(src, rel)
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return err
		}
		return os.WriteFile(dest, []byte(content), 0o644)
	}

	if err := mustRun(src, "init", "-q", "-b", "main", "."); err != nil {
		return nil, err
	}
	if err := write("README.md", "# fixture\nhello from the fixture repo\n"); err != nil {
		return nil, err
	}
	if err := mustRun(src, "add", "-A"); err != nil {
		return nil, err
	}
	if err := mustRun(src, "commit", "-qm", "fixture commit"); err != nil {
		return nil, err
	}
	if err := write("src/main.txt", "the answer is 42\n"); err != nil {
		return nil, err
	}
	if err := mustRun(src, "add", "-A"); err != nil {
		return nil, err
	}
	if err := mustRun(src, "commit", "-qm", "second commit"); err != nil {
		return nil, err
	}
	if err := mustRun(src, "tag", "-a", "v1.0", "-m", "release"); err != nil {
		return nil, err
	}

	// One bare clone, one full repack: exactly one pack pair.
	tmp, err := os.MkdirTemp("", "quill-seed-bare")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	bare := filepath.Join(tmp, "bare.git")
	if err := mustRun(src, "clone", "-q", "--bare", src, bare); err != nil {
		return nil, err
	}
	if err := mustRun(bare, "repack", "-a", "-d", "-q"); err != nil {
		return nil, err
	}
	packDir := filepath.Join(bare, "objects", "pack")
	entries, err := os.ReadDir(packDir)
	if err != nil {
		return nil, err
	}
	var packFile, idxFile string
	for _, e := range entries {
		switch {
		case strings.HasSuffix(e.Name(), ".pack"):
			packFile = filepath.Join(packDir, e.Name())
		case strings.HasSuffix(e.Name(), ".idx"):
			idxFile = filepath.Join(packDir, e.Name())
		}
	}
	if packFile == "" || idxFile == "" {
		return nil, fmt.Errorf("repack did not produce a pack+idx pair in %s: %v", packDir, entries)
	}
	base := strings.TrimSuffix(filepath.Base(packFile), ".pack")

	upload := func(key, path string) (string, int64, error) {
		b, err := os.ReadFile(path)
		if err != nil {
			return "", 0, err
		}
		key = RepoPrefix(tenantID, projectSlug, repoSlug) + key
		etag, err := store.Put(ctx, key, b, objectstore.PutOptions{IfNoneMatch: "*"})
		if err != nil {
			return "", 0, fmt.Errorf("upload %s: %w", key, err)
		}
		return etag, int64(len(b)), nil
	}
	packEtag, packBytes, err := upload("packs/"+base+".pack", packFile)
	if err != nil {
		return nil, err
	}
	idxEtag, idxBytes, err := upload("packs/"+base+".idx", idxFile)
	if err != nil {
		return nil, err
	}

	m := NewManifest(tenantID, "main")
	refOut, err := run(bare, "for-each-ref", "--format=%(refname) %(objectname) %(*objectname)")
	if err != nil {
		return nil, err
	}
	for _, line := range strings.Split(strings.TrimSpace(refOut), "\n") {
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		ref := ManifestRef{Name: parts[0], Target: parts[1]}
		if len(parts) > 2 && parts[2] != "" {
			ref.Peeled = parts[2]
		}
		m.Refs = append(m.Refs, ref)
	}
	m.Packs = []ManifestPack{{
		Object: "packs/" + base + ".pack", Bytes: packBytes, ETag: packEtag,
		IdxObject: "packs/" + base + ".idx", IdxBytes: idxBytes, IdxETag: idxEtag,
	}}
	m.StorageBytes = packBytes + idxBytes

	enc, err := m.Encode()
	if err != nil {
		return nil, err
	}
	if _, err := store.Put(ctx, ManifestKey(tenantID, projectSlug, repoSlug), []byte(enc), objectstore.PutOptions{IfNoneMatch: "*"}); err != nil {
		return nil, fmt.Errorf("write fixture manifest: %w", err)
	}
	return m, nil
}
