package gitengine

import "testing"

// TestLayoutKeys pins the bucket layout (irreversible): the manifest path and
// pack paths are the addresses every future tool and streamer must agree on.
// Slugs are path-escaped per segment so hostile repo slugs can never traverse
// outside the repo prefix.
func TestLayoutKeys(t *testing.T) {
	got := ManifestKey("5bde6bd0-b69c-4bd8-8b4e-b0a2599b1d31", "acme", "widgets")
	if got != "repos/5bde6bd0-b69c-4bd8-8b4e-b0a2599b1d31/acme/widgets/manifest.json" {
		t.Fatalf("manifest key = %q", got)
	}
	got = PackKey("5bde6bd0-b69c-4bd8-8b4e-b0a2599b1d31", "acme", "widgets", "0f5e9c2b", "pack")
	if got != "repos/5bde6bd0-b69c-4bd8-8b4e-b0a2599b1d31/acme/widgets/packs/0f5e9c2b.pack" {
		t.Fatalf("pack key = %q", got)
	}
	got = PackKey("5bde6bd0-b69c-4bd8-8b4e-b0a2599b1d31", "acme", "widgets", "0f5e9c2b", "idx")
	if got != "repos/5bde6bd0-b69c-4bd8-8b4e-b0a2599b1d31/acme/widgets/packs/0f5e9c2b.idx" {
		t.Fatalf("idx key = %q", got)
	}
	prefix := RepoPrefix("5bde6bd0-b69c-4bd8-8b4e-b0a2599b1d31", "acme", "widgets")
	if prefix != "repos/5bde6bd0-b69c-4bd8-8b4e-b0a2599b1d31/acme/widgets/" {
		t.Fatalf("repo prefix = %q", prefix)
	}
}

// TestLayoutKeysEscapeSlugs ensures slug segments are escaped: a project or
// repo slug containing '/' (or '.'/encoding tricks) must land inside exactly
// one bucket prefix, never a sibling's.
func TestLayoutKeysEscapeSlugs(t *testing.T) {
	key := ManifestKey("t", "a/b", "../../other")
	if key != "repos/t/a%2Fb/..%2F..%2Fother/manifest.json" {
		t.Fatalf("escaped key = %q", key)
	}
}
