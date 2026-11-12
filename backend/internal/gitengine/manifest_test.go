package gitengine

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// goldenManifest pins the v1 wire format. The manifest is THE irreversible
// format decision of the serverless epic: once real repos live in the bucket
// every future reader must parse what this test pins. Any change here needs a
// manifest version bump + a migration story, not an edit.
//
// Field order is part of the golden (deterministic encode keeps objects
// diffable and etags meaningful for the same logical content).
const goldenManifest = `{
  "version": 1,
  "tenant_id": "5bde6bd0-b69c-4bd8-8b4e-b0a2599b1d31",
  "default_branch": "main",
  "refs": [
    {
      "name": "refs/heads/main",
      "target": "52cb8b78c514cb6333870978d68c896db25d7573"
    },
    {
      "name": "refs/tags/v1.0",
      "target": "1d0b4b78c514cb6333870978d68c896db25d7573",
      "peeled": "9d0b4b78c514cb6333870978d68c896db25d7573"
    }
  ],
  "packs": [
    {
      "object": "packs/0f5e9c2b.pack",
      "bytes": 2048,
      "etag": "\"a7743b\"",
      "idx_object": "packs/0f5e9c2b.idx",
      "idx_bytes": 512,
      "idx_etag": "\"b8854c\""
    }
  ],
  "push_history": [
    {
      "pushed_by": "niels",
      "tenant_id": "5bde6bd0-b69c-4bd8-8b4e-b0a2599b1d31",
      "pushed_at": "2026-11-12T10:00:00Z",
      "pushed_sha": "52cb8b78c514cb6333870978d68c896db25d7573"
    }
  ],
  "updated_at": "2026-11-12T10:00:01Z",
  "storage_bytes": 2048
}`

var goldenUpdatedAt = time.Date(2026, 11, 12, 10, 0, 1, 0, time.UTC)

func TestManifestEncodeGolden(t *testing.T) {
	m := NewManifest("5bde6bd0-b69c-4bd8-8b4e-b0a2599b1d31", "main")
	m.Refs = []ManifestRef{
		{Name: "refs/heads/main", Target: "52cb8b78c514cb6333870978d68c896db25d7573"},
		{Name: "refs/tags/v1.0", Target: "1d0b4b78c514cb6333870978d68c896db25d7573", Peeled: "9d0b4b78c514cb6333870978d68c896db25d7573"},
	}
	m.Packs = []ManifestPack{{
		Object: "packs/0f5e9c2b.pack", Bytes: 2048, ETag: "\"a7743b\"",
		IdxObject: "packs/0f5e9c2b.idx", IdxBytes: 512, IdxETag: "\"b8854c\"",
	}}
	m.PushHistory = []ManifestPush{{
		PushedBy: "niels", TenantID: "5bde6bd0-b69c-4bd8-8b4e-b0a2599b1d31",
		PushedAt:  time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC),
		PushedSha: "52cb8b78c514cb6333870978d68c896db25d7573",
	}}
	m.UpdatedAt = goldenUpdatedAt
	m.StorageBytes = 2048

	got, err := m.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if got != goldenManifest+"\n" {
		t.Fatalf("encode drifted from golden:\n--- got ---\n%s\n--- want ---\n%s", got, goldenManifest)
	}
}

func TestManifestDecodeGoldenLenient(t *testing.T) {
	m, err := DecodeManifest(strings.NewReader(goldenManifest))
	if err != nil {
		t.Fatalf("DecodeManifest(golden): %v", err)
	}
	if m.Version != ManifestVersion {
		t.Fatalf("version = %d", m.Version)
	}
	if m.TenantID != "5bde6bd0-b69c-4bd8-8b4e-b0a2599b1d31" {
		t.Fatalf("tenant_id = %q", m.TenantID)
	}
	if len(m.Refs) != 2 || m.Refs[1].Peeled != "9d0b4b78c514cb6333870978d68c896db25d7573" {
		t.Fatalf("refs = %+v", m.Refs)
	}
	if len(m.Packs) != 1 || m.Packs[0].IdxBytes != 512 {
		t.Fatalf("packs = %+v", m.Packs)
	}
	if len(m.PushHistory) != 1 || m.PushHistory[0].PushedBy != "niels" {
		t.Fatalf("push_history = %+v", m.PushHistory)
	}
	if m.StorageBytes != 2048 {
		t.Fatalf("storage_bytes = %d", m.StorageBytes)
	}

	// Round-trip: decode(encode(m)) must be byte-stable.
	re, err := m.Encode()
	if err != nil {
		t.Fatalf("re-encode: %v", err)
	}
	if re != goldenManifest+"\n" {
		t.Fatalf("round-trip drifted from golden:\n%s", re)
	}

	// Unknown fields are ignored (forward compatibility: old readers must
	// parse newer manifests, since version only guarantees the fields here).
	withUnknown := strings.Replace(goldenManifest,
		`"version": 1,`, `"version": 1, "future_field": {"x": 1},`, 1)
	if _, err := DecodeManifest(strings.NewReader(withUnknown)); err != nil {
		t.Fatalf("lenient decode must tolerate unknown fields: %v", err)
	}
}

func TestManifestDecodeRejectsFutureVersion(t *testing.T) {
	future := strings.Replace(goldenManifest, `"version": 1,`, `"version": 2,`, 1)
	if _, err := DecodeManifest(strings.NewReader(future)); err == nil {
		t.Fatal("a version newer than the parser must fail closed, got nil")
	}
}

func TestManifestDecodeRejectsGarbage(t *testing.T) {
	for name, in := range map[string]string{
		"empty":       ``,
		"not-json":    `nope`,
		"wrong-types": `{"version":1,"refs":"no"}`,
	} {
		if _, err := DecodeManifest(strings.NewReader(in)); err == nil {
			t.Fatalf("%s: decode must fail, got nil", name)
		}
	}
}

func TestManifestValidate(t *testing.T) {
	m := NewManifest("t", "main")
	if err := m.Validate(); err != nil {
		t.Fatalf("fresh empty manifest must validate: %v", err)
	}
	// Bad ref names/pairs are rejected at the referee boundary, not mid-push.
	m.Refs = []ManifestRef{{Name: "refs/heads/main"}}
	if err := m.Validate(); err == nil {
		t.Fatal("ref without target must not validate")
	}
	// Pack entries must be well-formed (streaming later relies on them).
	m.Refs = nil
	m.Packs = []ManifestPack{{Object: "packs/x.pack"}}
	if err := m.Validate(); err == nil {
		t.Fatal("pack without etag must not validate")
	}
}

// TestManifestPushHistoryBounds pins the push-history tail behaviour: the
// manifest stays small (it is read at every cold start), so entries beyond the
// tail cap are dropped oldest-first.
func TestManifestPushHistoryBounds(t *testing.T) {
	m := NewManifest("t", "main")
	for i := 0; i < PushHistoryCap+10; i++ {
		m.RecordPush("user", "t", time.Unix(int64(i), 0).UTC(), "sha")
	}
	if len(m.PushHistory) != PushHistoryCap {
		t.Fatalf("push history = %d entries, want capped at %d", len(m.PushHistory), PushHistoryCap)
	}
	// Oldest dropped: the tail holds the most recent entries.
	if want := time.Unix(int64(PushHistoryCap+9), 0).UTC(); !m.PushHistory[len(m.PushHistory)-1].PushedAt.Equal(want) {
		t.Fatalf("tail = %+v, want newest entry %v", m.PushHistory[len(m.PushHistory)-1], want)
	}
}

// jsonMarshalIndent is used by Encode; blowing up on unsupported types would be
// a programming error, so encode failures are the only path to an error.
var _ = json.Marshal
