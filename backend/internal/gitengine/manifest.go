// Package gitengine is Quill's own git storage engine (serverless epic,
// Phase B): the Object Storage bucket IS the repository, any running instance
// is a disposable cache. Truth for refs lives in Postgres (row-per-repo
// compare-and-swap, internal/store git_state); the manifest in the bucket is
// a reproducible materialized view of that row, written through If-None-Match/
// If-Match conditional writes (an optional fast path — Postgres is the commit
// point; see DECISIONS.md 2026-11-11).
//
// B1 scope: bucket layout, manifest format, provisioning. Read/write HTTP
// paths land in B2/B3.
package gitengine

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"
)

// ManifestVersion is the current manifest schema version. Version 1.
const ManifestVersion = 1

// PushHistoryCap bounds the manifest's push-history tail: manifests are read
// on every cold start, so they stay small by design; the audit log keeps the
// full history.
const PushHistoryCap = 16

// ManifestRef is one ref with its current target. Peeled is set only for
// annotated tags (the commit the tag object points at).
type ManifestRef struct {
	Name   string `json:"name"`
	Target string `json:"target"`
	Peeled string `json:"peeled,omitempty"`
}

// ManifestPack records one packfile in the bucket. Packs stay whole,
// individually addressable objects (never concatenations) so later streaming
// can range-request them — this field set is part of the irreversible layout
// contract: object key, pack size, etag, and the sidecar idx.
type ManifestPack struct {
	Object    string `json:"object"`
	Bytes     int64  `json:"bytes"`
	ETag      string `json:"etag"`
	IdxObject string `json:"idx_object,omitempty"`
	IdxBytes  int64  `json:"idx_bytes,omitempty"`
	IdxETag   string `json:"idx_etag,omitempty"`
}

// ManifestPush is one push-history tail entry (tenant-stamped: metering never
// depends on joins surviving tenant deletion).
type ManifestPush struct {
	PushedBy  string    `json:"pushed_by"`
	TenantID  string    `json:"tenant_id"`
	PushedAt  time.Time `json:"pushed_at"`
	PushedSha string    `json:"pushed_sha"`
}

// Manifest is THE pointer to a repository's state in the bucket. Versioned
// from day one; decoders are lenient about unknown fields (std json) and fail
// closed on versions they do not understand.
type Manifest struct {
	Version       int            `json:"version"`
	TenantID      string         `json:"tenant_id"`
	DefaultBranch string         `json:"default_branch"`
	Refs          []ManifestRef  `json:"refs"`
	Packs         []ManifestPack `json:"packs"`
	PushHistory   []ManifestPush `json:"push_history"`
	UpdatedAt     time.Time      `json:"updated_at"`
	StorageBytes  int64          `json:"storage_bytes"`
}

// NewManifest returns a fresh, empty, valid manifest.
func NewManifest(tenantID, defaultBranch string) *Manifest {
	return &Manifest{
		Version:       ManifestVersion,
		TenantID:      tenantID,
		DefaultBranch: defaultBranch,
		UpdatedAt:     time.Now().UTC(),
	}
}

// Encode deterministically serializes the manifest (key order = struct order;
// stable output keeps etags meaningful for identical logical content).
func (m *Manifest) Encode() (string, error) {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return "", fmt.Errorf("encode manifest: %w", err)
	}
	return string(b) + "\n", nil
}

// DecodeManifest parses a manifest, tolerating unknown fields but refusing
// schemas newer than this build understands (fail closed: a v2 manifest could
// change semantics this reader relies on).
func DecodeManifest(r io.Reader) (*Manifest, error) {
	dec := json.NewDecoder(r)
	var m Manifest
	if err := dec.Decode(&m); err != nil {
		return nil, fmt.Errorf("decode manifest: %w", err)
	}
	if m.Version > ManifestVersion {
		return nil, fmt.Errorf("manifest version %d is newer than this build supports (%d): refusing to interpret", m.Version, ManifestVersion)
	}
	if m.Version < 1 {
		return nil, errors.New("manifest version must be a positive integer")
	}
	return &m, nil
}

// Validate asserts the invariants every persisted manifest must hold.
func (m *Manifest) Validate() error {
	if m.Version != ManifestVersion {
		return fmt.Errorf("manifest version = %d, must be %d", m.Version, ManifestVersion)
	}
	if strings.TrimSpace(m.TenantID) == "" {
		return errors.New("manifest tenant_id is required")
	}
	if strings.TrimSpace(m.DefaultBranch) == "" {
		return errors.New("manifest default_branch is required")
	}
	seen := map[string]bool{}
	for _, r := range m.Refs {
		if !strings.HasPrefix(r.Name, "refs/") {
			return fmt.Errorf("ref name %q must start with refs/", r.Name)
		}
		if seen[r.Name] {
			return fmt.Errorf("duplicate ref %q", r.Name)
		}
		seen[r.Name] = true
		if r.Target == "" {
			return fmt.Errorf("ref %q has no target", r.Name)
		}
	}
	for i, p := range m.Packs {
		if !strings.HasPrefix(p.Object, "packs/") {
			return fmt.Errorf("pack %d object %q must live under packs/", i, p.Object)
		}
		if p.Bytes < 0 || p.IdxBytes < 0 {
			return fmt.Errorf("pack %d has negative sizes", i)
		}
		if p.ETag == "" {
			return fmt.Errorf("pack %d is missing its etag (streaming relies on it)", i)
		}
		if (p.IdxObject == "") != (p.IdxETag == "") {
			return fmt.Errorf("pack %d idx object/etag must be set together", i)
		}
	}
	if m.StorageBytes < 0 {
		return errors.New("negative storage_bytes")
	}
	return nil
}

// RecordPush appends a push entry, drops the tail oldest-first beyond the cap.
func (m *Manifest) RecordPush(pushedBy, tenantID string, pushedAt time.Time, sha string) {
	m.PushHistory = append(m.PushHistory, ManifestPush{
		PushedBy: pushedBy, TenantID: tenantID, PushedAt: pushedAt, PushedSha: sha,
	})
	if over := len(m.PushHistory) - PushHistoryCap; over > 0 {
		m.PushHistory = m.PushHistory[over:]
	}
	m.UpdatedAt = time.Now().UTC()
}

// escape returns a bucket path segment that can never traverse ('/' and other
// reserved bytes percent-encoded by net/url).
func escape(segment string) string { return url.PathEscape(segment) }

// ManifestKey is the object key of a repo's manifest.
func ManifestKey(tenantID, projectSlug, repoSlug string) string {
	return RepoPrefix(tenantID, projectSlug, repoSlug) + "manifest.json"
}

// PackKey is the object key of one packfile or its idx sidecar
// (kind = "pack" | "idx").
func PackKey(tenantID, projectSlug, repoSlug, sha, kind string) string {
	return RepoPrefix(tenantID, projectSlug, repoSlug) + "packs/" + escape(sha) + "." + kind
}

// RepoPrefix is the object-key prefix every object of one repo lives under.
func RepoPrefix(tenantID, projectSlug, repoSlug string) string {
	return "repos/" + escape(tenantID) + "/" + escape(projectSlug) + "/" + escape(repoSlug) + "/"
}
