package platform

import "testing"

// TestNormalizeSources verifies the allowed-source-branch normalization used by
// environment policies: trim whitespace, drop blanks, and de-duplicate while
// preserving first-seen order. This keeps stored globs stable and prevents blank
// entries from reaching glob validation.
func TestNormalizeSources(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{"nil", nil, []string{}},
		{"empty", []string{}, []string{}},
		{"trims and drops blanks", []string{" main ", "", "  ", "release/*"}, []string{"main", "release/*"}},
		{"dedupes preserving order", []string{"main", "release/*", "main", " release/* "}, []string{"main", "release/*"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := normalizeSources(tc.in)
			if len(got) != len(tc.want) {
				t.Fatalf("len mismatch: got %v want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("at %d: got %q want %q (%v)", i, got[i], tc.want[i], got)
				}
			}
		})
	}
}
