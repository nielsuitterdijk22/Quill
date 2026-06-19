package server

import (
	"crypto/sha256"
	"fmt"
	"sync"
	"time"
)

// markupTTL is how long a rendered HTML result is cached.
const markupTTL = 5 * time.Minute

// markupCacheMaxSize caps the number of entries to prevent unbounded growth.
const markupCacheMaxSize = 512

type markupEntry struct {
	html      string
	expiresAt time.Time
}

// markupCache is a bounded, in-process TTL cache for rendered markdown HTML.
// It is keyed on (project slug, repo slug, sha256 of the raw markdown text)
// so identical content in the same repository context is rendered only once
// per TTL window rather than on every page load.
type markupCache struct {
	mu      sync.Mutex
	entries map[string]markupEntry
}

func newMarkupCache() *markupCache {
	return &markupCache{entries: make(map[string]markupEntry)}
}

func markupCacheKey(projectSlug, repoSlug, text string) string {
	h := sha256.Sum256([]byte(text))
	return fmt.Sprintf("%s/%s/%x", projectSlug, repoSlug, h)
}

// get returns the cached HTML and true if the entry exists and has not expired.
func (c *markupCache) get(key string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[key]
	if !ok || time.Now().After(e.expiresAt) {
		delete(c.entries, key)
		return "", false
	}
	return e.html, true
}

// set stores html under key with a fixed TTL. If the cache is at capacity it
// evicts all expired entries first; if still full, it clears the whole cache
// (simple eviction strategy — renders are cheap to re-derive).
func (c *markupCache) set(key, html string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.entries) >= markupCacheMaxSize {
		now := time.Now()
		for k, e := range c.entries {
			if now.After(e.expiresAt) {
				delete(c.entries, k)
			}
		}
		if len(c.entries) >= markupCacheMaxSize {
			c.entries = make(map[string]markupEntry)
		}
	}
	c.entries[key] = markupEntry{html: html, expiresAt: time.Now().Add(markupTTL)}
}
