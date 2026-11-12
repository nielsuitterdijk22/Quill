package gitengine

import (
	"bytes"
	"context"
	"fmt"

	"github.com/nielsuitterdijk22/quill/internal/objectstore"
)

// ObjectStore is the minimal surface the engine needs from bucket storage;
// objectstore.Client (S3-compatible) and objectstore.MemStore implement it.
type ObjectStore interface {
	Put(ctx context.Context, key string, body []byte, opts objectstore.PutOptions) (etag string, err error)
	Get(ctx context.Context, key string) (body []byte, etag string, err error)
	Head(ctx context.Context, key string) (size int64, etag string, err error)
	Delete(ctx context.Context, key string) error
}

// Engine owns the bucket side of the git engine: manifests in, manifests out.
// Ref truth lives in Postgres (internal/store git_state); the engine's writes
// are the materialized view of the referee.
type Engine struct {
	store ObjectStore
}

// New builds an engine over an object store.
func New(store ObjectStore) *Engine {
	return &Engine{store: store}
}

// ProvisionRepo writes the initial empty manifest for a new repository:
// create-if-absent (If-None-Match: *). Callers create the Postgres referee row
// in the same logical operation; if that part fails, DeleteManifest provides
// the compensation (the manifest was empty — nothing is lost).
// Idempotency boundary: provisioning an existing repo fails with
// objectstore.ErrPreconditionFailed instead of resetting it. Provisioning
// validates + stamps the manifest, including UpdatedAt.
func (e *Engine) ProvisionRepo(ctx context.Context, tenantID, projectSlug, repoSlug, defaultBranch string) error {
	key := ManifestKey(tenantID, projectSlug, repoSlug)
	m := NewManifest(tenantID, defaultBranch)
	if err := m.Validate(); err != nil {
		return err
	}
	enc, err := m.Encode()
	if err != nil {
		return err
	}
	if _, err := e.store.Put(ctx, key, []byte(enc), objectstore.PutOptions{IfNoneMatch: "*"}); err != nil {
		return fmt.Errorf("write initial manifest: %w", err)
	}
	return nil
}

// LoadManifest fetches and decodes a repo's manifest plus its current etag.
func (e *Engine) LoadManifest(ctx context.Context, tenantID, projectSlug, repoSlug string) (*Manifest, string, error) {
	body, etag, err := e.store.Get(ctx, ManifestKey(tenantID, projectSlug, repoSlug))
	if err != nil {
		return nil, "", fmt.Errorf("read manifest: %w", err)
	}
	m, err := DecodeManifest(bytes.NewReader(body))
	if err != nil {
		return nil, "", err
	}
	if err := m.Validate(); err != nil {
		return nil, "", err
	}
	return m, etag, nil
}

// WriteManifest persists the manifest. ifMatch is the fast-path precondition:
// when non-empty the write only lands if the bucket still holds that etag
// (ErrPreconditionFailed = lost race; caller retries against the referee).
// When empty, the write is unconditional — only the Postgres referee path may
// use that, never a speculative writer.
func (e *Engine) WriteManifest(ctx context.Context, tenantID, projectSlug, repoSlug string, m *Manifest, ifMatch string) (string, error) {
	if err := m.Validate(); err != nil {
		return "", err
	}
	enc, err := m.Encode()
	if err != nil {
		return "", err
	}
	opts := objectstore.PutOptions{IfMatch: ifMatch}
	etag, err := e.store.Put(ctx, ManifestKey(tenantID, projectSlug, repoSlug), []byte(enc), opts)
	if err != nil {
		return "", fmt.Errorf("write manifest: %w", err)
	}
	return etag, nil
}

// DeleteManifest removes the manifest — the compensation path when the
// Postgres side of provisioning failed and the manifest was still empty.
func (e *Engine) DeleteManifest(ctx context.Context, tenantID, projectSlug, repoSlug string) error {
	err := e.store.Delete(ctx, ManifestKey(tenantID, projectSlug, repoSlug))
	if err != nil {
		return fmt.Errorf("delete manifest: %w", err)
	}
	return nil
}
