package workitemrefs

import (
	"reflect"
	"testing"
)

func TestExtractKeys(t *testing.T) {
	cases := []struct {
		name    string
		sources []string
		want    []string
	}{
		{
			name:    "simple key",
			sources: []string{"fixes ABC-123 in the login flow"},
			want:    []string{"ABC-123"},
		},
		{
			name:    "case-insensitive scan, uppercased output",
			sources: []string{"abc-123 and Def-45 both count"},
			want:    []string{"ABC-123", "DEF-45"},
		},
		{
			name:    "dedupe within and across sources",
			sources: []string{"ABC-123 ABC-123", "abc-123", "XY-7"},
			want:    []string{"ABC-123", "XY-7"},
		},
		{
			name:    "digits allowed after first letter",
			sources: []string{"P2-9 relates to A1B2-100"},
			want:    []string{"P2-9", "A1B2-100"},
		},
		{
			// The contract is explicit: false-positive-looking strings are still
			// extracted; dropping keys that resolve to nothing is Tempo's job.
			name:    "false positives like UTF-8 and SHA-256 are still extracted",
			sources: []string{"decode UTF-8 then hash with SHA-256"},
			want:    []string{"UTF-8", "SHA-256"},
		},
		{
			name:    "no key present",
			sources: []string{"just a plain commit message", "another-line"},
			want:    nil,
		},
		{
			// Case-insensitive scanning means the whole token xABC-123 reads as the
			// key XABC-123 — like UTF-8, extracting it is fine; Tempo drops keys
			// that resolve to nothing. Trailing letters after the digits, however,
			// break the key shape entirely.
			name:    "trailing letters break the match; leading letters join it",
			sources: []string{"xABC-123 and ABC-123x"},
			want:    []string{"XABC-123"},
		},
		{
			name:    "must start with a letter",
			sources: []string{"123-456 is not a key"},
			want:    nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ExtractKeys(tc.sources...)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("ExtractKeys(%q) = %v, want %v", tc.sources, got, tc.want)
			}
		})
	}
}

func TestPushRefs(t *testing.T) {
	commits := []Commit{
		{
			SHA:     "abc123sha",
			Message: "ABC-1 fix the thing\n\nlonger body here",
			URL:     "https://forge.example/acme/web/commit/abc123sha",
			Author:  "alice",
		},
		{
			SHA:     "def456sha",
			Message: "routine cleanup, no key",
			URL:     "https://forge.example/acme/web/commit/def456sha",
			Author:  "bob",
		},
	}

	// The branch name is a key source for push events, so a branch named after a
	// work item (ABC-2-feature) links every commit pushed on it — including the
	// commit whose own message mentions no key.
	refs := PushRefs("web", "ABC-2-feature", commits)
	if len(refs) != 2 {
		t.Fatalf("expected 2 refs (branch key attaches to keyless commit too), got %d: %+v", len(refs), refs)
	}
	r := refs[0]
	if r.RefType != "commit" {
		t.Errorf("refType = %q, want commit", r.RefType)
	}
	if r.RepoSlug != "web" {
		t.Errorf("repoSlug = %q, want web", r.RepoSlug)
	}
	if r.ExternalRef != "abc123sha" {
		t.Errorf("externalRef = %q, want the sha", r.ExternalRef)
	}
	if r.URL != "https://forge.example/acme/web/commit/abc123sha" {
		t.Errorf("url = %q", r.URL)
	}
	if r.Title != "ABC-1 fix the thing" {
		t.Errorf("title = %q, want first line only", r.Title)
	}
	if r.State != "" {
		t.Errorf("state = %q, want empty for a commit", r.State)
	}
	if r.Author != "alice" {
		t.Errorf("author = %q, want alice", r.Author)
	}
	// Branch key is attached to the commit ref alongside the message key.
	want := []string{"ABC-1", "ABC-2"}
	if !reflect.DeepEqual(r.WorkItemKeys, want) {
		t.Errorf("workItemKeys = %v, want %v (message + branch)", r.WorkItemKeys, want)
	}
	// The keyless commit is carried by the branch key alone.
	r2 := refs[1]
	if r2.ExternalRef != "def456sha" || r2.Author != "bob" {
		t.Errorf("second ref mismatch: %+v", r2)
	}
	if !reflect.DeepEqual(r2.WorkItemKeys, []string{"ABC-2"}) {
		t.Errorf("second ref workItemKeys = %v, want [ABC-2] (branch only)", r2.WorkItemKeys)
	}
}

func TestPushRefsNoKeys(t *testing.T) {
	commits := []Commit{{SHA: "x", Message: "no key here", Author: "a"}}
	if refs := PushRefs("web", "main", commits); len(refs) != 0 {
		t.Fatalf("expected no refs, got %+v", refs)
	}
}

func TestPullRequestRef(t *testing.T) {
	pr := PullRequest{
		Number: 42,
		Title:  "DEF-9: add search",
		Body:   "closes abc-100\nand mentions XY-7",
		State:  "open",
		URL:    "https://forge.example/acme/web/pulls/42",
		Branch: "feature/DEF-9",
		Author: "carol",
	}
	r, ok := PullRequestRef("web", pr)
	if !ok {
		t.Fatal("expected a ref to be built")
	}
	if r.RefType != "pr" {
		t.Errorf("refType = %q, want pr", r.RefType)
	}
	if r.ExternalRef != "42" {
		t.Errorf("externalRef = %q, want 42", r.ExternalRef)
	}
	if r.Title != "DEF-9: add search" {
		t.Errorf("title = %q", r.Title)
	}
	if r.State != "open" {
		t.Errorf("state = %q, want open", r.State)
	}
	if r.URL != "https://forge.example/acme/web/pulls/42" {
		t.Errorf("url = %q", r.URL)
	}
	if r.Author != "carol" {
		t.Errorf("author = %q, want carol", r.Author)
	}
	// title (DEF-9), body (ABC-100 uppercased, XY-7), branch (DEF-9 deduped).
	want := []string{"DEF-9", "ABC-100", "XY-7"}
	if !reflect.DeepEqual(r.WorkItemKeys, want) {
		t.Errorf("workItemKeys = %v, want %v", r.WorkItemKeys, want)
	}
}

func TestPullRequestRefNoKeys(t *testing.T) {
	pr := PullRequest{Number: 1, Title: "tidy up", Body: "nothing", Branch: "main"}
	if _, ok := PullRequestRef("web", pr); ok {
		t.Fatal("expected no ref when the PR mentions no work item")
	}
}
