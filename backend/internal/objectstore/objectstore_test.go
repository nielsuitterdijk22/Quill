package objectstore

import (
	"context"
	"errors"
	"testing"
)

func TestMemStoreLifecycle(t *testing.T) {
	m := NewMemStore()
	ctx := context.Background()

	if _, _, err := m.Get(ctx, "k"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("get missing = %v, want ErrNotFound", err)
	}
	etag, err := m.Put(ctx, "k", []byte("v1"), PutOptions{})
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	body, gotEtag, err := m.Get(ctx, "k")
	if err != nil || string(body) != "v1" || gotEtag != etag {
		t.Fatalf("get = %q %q %v", body, gotEtag, err)
	}

	// If-None-Match:* refuses to overwrite existing keys.
	if _, err := m.Put(ctx, "k", []byte("v2"), PutOptions{IfNoneMatch: "*"}); !errors.Is(err, ErrPreconditionFailed) {
		t.Fatalf("create-if-absent over existing = %v, want ErrPreconditionFailed", err)
	}
	// If-None-Match:* on a fresh key succeeds.
	if _, err := m.Put(ctx, "other", []byte("x"), PutOptions{IfNoneMatch: "*"}); err != nil {
		t.Fatalf("create-if-absent fresh: %v", err)
	}
	// If-Match with the current etag succeeds.
	newEtag, err := m.Put(ctx, "k", []byte("v2"), PutOptions{IfMatch: etag})
	if err != nil {
		t.Fatalf("put with current etag: %v", err)
	}
	if newEtag == etag {
		t.Fatal("etag must change on content change")
	}
	// If-Match with a stale etag fails and does not apply.
	if _, err := m.Put(ctx, "k", []byte("v3"), PutOptions{IfMatch: etag}); !errors.Is(err, ErrPreconditionFailed) {
		t.Fatalf("put with stale etag = %v, want ErrPreconditionFailed", err)
	}
	body, _, _ = m.Get(ctx, "k")
	if string(body) != "v2" {
		t.Fatalf("stale put must not apply; got %q", body)
	}

	size, _, err := m.Head(ctx, "k")
	if err != nil || size != 2 {
		t.Fatalf("head = %d %v, want size 2", size, err)
	}
	if err := m.Delete(ctx, "k"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, _, err := m.Get(ctx, "k"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("get after delete = %v, want ErrNotFound", err)
	}
	if err := m.Delete(ctx, "k"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("delete missing = %v, want ErrNotFound", err)
	}
}

// TestS3LiveRoundtrip runs the same conditional-write contract against a real
// S3-compatible endpoint (Scaleway Object Storage locally; validated live in
// the PR0 spike). Gated so CI never needs bucket credentials:
//
//	QUILL_TEST_S3_ENDPOINT=https://s3.fr-par.scw.cloud \
//	QUILL_TEST_S3_REGION=fr-par QUILL_TEST_S3_BUCKET=<bucket> \
//	QUILL_TEST_S3_ACCESS_KEY=<ak> QUILL_TEST_S3_SECRET_KEY=<sk> \
//	go test ./internal/objectstore/
func TestS3LiveRoundtrip(t *testing.T) {
	cfg := ConfigFromEnv("QUILL_TEST_S3_")
	if cfg.Endpoint == "" || cfg.Bucket == "" {
		t.Skip("QUILL_TEST_S3_ENDPOINT / QUILL_TEST_S3_BUCKET not set; skipping live S3 test")
	}
	c := New(cfg)
	ctx := context.Background()
	key := "test/git-engine-live/roundtrip-" + t.Name()

	t.Cleanup(func() { _ = c.Delete(ctx, key) })

	// Create-if-absent.
	etag, err := c.Put(ctx, key, []byte("v1"), PutOptions{IfNoneMatch: "*"})
	if err != nil {
		t.Fatalf("put if-none-match: %v", err)
	}
	if etag == "" {
		t.Fatal("put must return the object's etag")
	}
	// Duplicate create must fail with the documented 412.
	if _, err := c.Put(ctx, key, []byte("v2"), PutOptions{IfNoneMatch: "*"}); !errors.Is(err, ErrPreconditionFailed) {
		t.Fatalf("duplicate create = %v, want ErrPreconditionFailed", err)
	}
	// Conditional fetch + replace.
	body, gotEtag, err := c.Get(ctx, key)
	if err != nil || string(body) != "v1" || gotEtag != etag {
		t.Fatalf("get = %q %q %v", body, gotEtag, err)
	}
	newEtag, err := c.Put(ctx, key, []byte("v2"), PutOptions{IfMatch: etag})
	if err != nil || newEtag == etag {
		t.Fatalf("if-match put = %q, %v", newEtag, err)
	}
	if _, err := c.Put(ctx, key, []byte("v3"), PutOptions{IfMatch: etag}); !errors.Is(err, ErrPreconditionFailed) {
		t.Fatalf("stale if-match = %v, want ErrPreconditionFailed", err)
	}
	body, _, _ = c.Get(ctx, key)
	if string(body) != "v2" {
		t.Fatalf("stale if-match must not apply; got %q", body)
	}
	size, _, err := c.Head(ctx, key)
	if err != nil || size != 2 {
		t.Fatalf("head = %d %v, want size 2", size, err)
	}
	if err := c.Delete(ctx, key); err != nil {
		t.Fatalf("delete: %v", err)
	}
}

func TestCanonicalQuery(t *testing.T) {
	got := canonicalQuery(map[string]string{"list-type": "2", "prefix": "repos/t é/", "x": "a~b"})
	want := "list-type=2&prefix=repos%2Ft%20%C3%A9%2F&x=a~b"
	if got != want {
		t.Fatalf("canonicalQuery = %q, want %q", got, want)
	}
	if canonicalQuery(nil) != "" {
		t.Fatal("empty query must render empty")
	}
}

func TestMemStoreList(t *testing.T) {
	m := NewMemStore()
	ctx := context.Background()
	for _, k := range []string{"a/1", "a/2", "b/3"} {
		if _, err := m.Put(ctx, k, []byte("x"), PutOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	keys, err := m.List(ctx, "a/")
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 2 || keys[0] != "a/1" || keys[1] != "a/2" {
		t.Fatalf("List(a/) = %v", keys)
	}
	if keys, _ := m.List(ctx, "zzz"); len(keys) != 0 {
		t.Fatalf("List(zzz) = %v", keys)
	}
}
