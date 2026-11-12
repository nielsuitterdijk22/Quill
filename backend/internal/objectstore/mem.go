package objectstore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"sync"
)

// MemStore is an in-memory ObjectStore implementing the same conditional-write
// contract as the S3 client. It is exported so platform/engine tests run the
// full provisioning + manifest paths without bucket credentials — the engine
// takes a small interface and never knows which it is talking to.
type MemStore struct {
	mu   sync.Mutex
	objs map[string]memObject
}

type memObject struct {
	data []byte
	etag string
}

// NewMemStore returns an empty store.
func NewMemStore() *MemStore {
	return &MemStore{objs: map[string]memObject{}}
}

func etagOf(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// Get returns the object and its etag, or ErrNotFound.
func (m *MemStore) Get(ctx context.Context, key string) ([]byte, string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	o, ok := m.objs[key]
	if !ok {
		return nil, "", ErrNotFound
	}
	return o.data, o.etag, nil
}

// GetRange returns the inclusive byte range [lo, hi].
func (m *MemStore) GetRange(ctx context.Context, key string, lo, hi int64) ([]byte, error) {
	b, _, err := m.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	if lo < 0 || hi >= int64(len(b)) {
		return nil, ErrNotFound
	}
	return b[lo : hi+1], nil
}

// Head returns size and etag, or ErrNotFound.
func (m *MemStore) Head(ctx context.Context, key string) (int64, string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	o, ok := m.objs[key]
	if !ok {
		return 0, "", ErrNotFound
	}
	return int64(len(o.data)), o.etag, nil
}

// Put stores the body and honours the conditional-write options.
func (m *MemStore) Put(ctx context.Context, key string, body []byte, opts PutOptions) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	existing, exists := m.objs[key]
	switch {
	case opts.IfNoneMatch == "*":
		if exists {
			return "", ErrPreconditionFailed
		}
	case opts.IfMatch != "":
		if !exists || existing.etag != opts.IfMatch {
			return "", ErrPreconditionFailed
		}
	}
	etag := etagOf(body)
	m.objs[key] = memObject{data: append([]byte(nil), body...), etag: etag}
	return etag, nil
}

// Delete removes the object; ErrNotFound when absent.
func (m *MemStore) Delete(ctx context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.objs[key]; !ok {
		return ErrNotFound
	}
	delete(m.objs, key)
	return nil
}

// Keys returns the object keys, sorted — test assertions use this to prove
// exactly what was (not) provisioned.
func (m *MemStore) Keys() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	keys := make([]string, 0, len(m.objs))
	for k := range m.objs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// String is a small debug helper.
func (m *MemStore) String() string { return fmt.Sprintf("MemStore(%d objects)", len(m.Keys())) }
