package live

import (
	"strings"
	"testing"
)

// The sidebar's Support link points at Quill's own public issue tracker; any
// signed-in user (here the e2e fixture, not a member of the quill project)
// must be able to open it, the short form, and the new-issue form, both as a
// full page load and as the RSC fetch a sidebar click makes.
func TestWebSupportIssues(t *testing.T) {
	requireLive(t)
	c := webSession(t)
	get(t, c, appURL+"/pulls", nil) // a first page, as a signed-in user would have
	for p, want := range map[string]string{
		"/projects/nuitterdijk/repos/quill/issues":     "Open</a>",
		"/nuitterdijk/quill/issues":                    "Open</a>",
		"/projects/nuitterdijk/repos/quill/issues/new": "<form",
	} {
		st, hdr, body := get(t, c, appURL+p, nil)
		if st != 200 || !strings.Contains(string(body), want) {
			t.Errorf("%s: %d (location %q), missing %q", p, st, hdr.Get("Location"), want)
		}
		st, hdr, _ = get(t, c, appURL+p, map[string]string{"RSC": "1"})
		if st != 200 || !strings.HasPrefix(hdr.Get("Content-Type"), "text/x-component") {
			t.Errorf("%s (client navigation): %d %s", p, st, hdr.Get("Content-Type"))
		}
	}
}

func snippet(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}
