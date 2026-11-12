package gitengine

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/nielsuitterdijk22/quill/internal/objectstore"
)

func TestProvisionRepoFresh(t *testing.T) {
	mem := objectstore.NewMemStore()
	eng := New(mem)
	ctx := context.Background()

	// Nothing at the key before provisioning.
	key := ManifestKey("t1", "acme", "widgets")
	if _, _, err := mem.Get(ctx, key); !errors.Is(err, objectstore.ErrNotFound) {
		t.Fatalf("pre-provision Get = %v, want ErrNotFound", err)
	}

	if err := eng.ProvisionRepo(ctx, "t1", "acme", "widgets", "main"); err != nil {
		t.Fatalf("ProvisionRepo: %v", err)
	}
	body, _, err := mem.Get(ctx, key)
	if err != nil {
		t.Fatalf("post-provision Get: %v", err)
	}
	m, err := DecodeManifest(strings.NewReader(string(body)))
	if err != nil {
		t.Fatalf("decode provisioned manifest: %v", err)
	}
	if m.Version != ManifestVersion || m.TenantID != "t1" || m.DefaultBranch != "main" {
		t.Fatalf("manifest = %+v", m)
	}
	if len(m.Refs) != 0 || len(m.Packs) != 0 || m.StorageBytes != 0 {
		t.Fatalf("fresh manifest must be empty: %+v", m)
	}
}

// TestProvisionRepoDoubleFails pins the idempotency boundary: the manifest is
// written with If-None-Match:*, so a double provision (retry after partial
// failure, or a slug race that somehow got this far) fails loudly instead of
// silently resetting an existing repository.
func TestProvisionRepoDoubleFails(t *testing.T) {
	mem := objectstore.NewMemStore()
	eng := New(mem)
	ctx := context.Background()

	if err := eng.ProvisionRepo(ctx, "t1", "acme", "widgets", "main"); err != nil {
		t.Fatalf("first provision: %v", err)
	}
	if err := eng.ProvisionRepo(ctx, "t1", "acme", "widgets", "main"); !errors.Is(err, objectstore.ErrPreconditionFailed) {
		t.Fatalf("second provision = %v, want ErrPreconditionFailed", err)
	}
}

// TestManifestRoundTripGitEngine writes a manifest back with If-Match fast
// path semantics: correct etag succeeds and returns the new etag; stale etag is
// a typed precondition failure the caller maps to a lost race (retry).
func TestManifestRoundTripWithIfMatch(t *testing.T) {
	mem := objectstore.NewMemStore()
	eng := New(mem)
	ctx := context.Background()

	if err := eng.ProvisionRepo(ctx, "t1", "acme", "widgets", "main"); err != nil {
		t.Fatalf("provision: %v", err)
	}
	m, etag, err := eng.LoadManifest(ctx, "t1", "acme", "widgets")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	m.Refs = []ManifestRef{{Name: "refs/heads/main", Target: "52cb8b78c514cb6333870978d68c896db25d7573"}}
	if _, err := eng.WriteManifest(ctx, "t1", "acme", "widgets", m, etag); err != nil {
		t.Fatalf("write with current etag: %v", err)
	}
	if _, err := eng.WriteManifest(ctx, "t1", "acme", "widgets", m, etag); !errors.Is(err, objectstore.ErrPreconditionFailed) {
		t.Fatalf("write with stale etag = %v, want ErrPreconditionFailed", err)
	}
	// The stale write must not have applied.
	got, _, err := eng.LoadManifest(ctx, "t1", "acme", "widgets")
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if len(got.Refs) != 1 || got.Refs[0].Target != "52cb8b78c514cb6333870978d68c896db25d7573" {
		t.Fatalf("refs after stale write = %+v", got.Refs)
	}
}
