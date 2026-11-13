package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// D2 cutover: git.<domain> points at the api container; requests on that host
// are served by the /git smart-HTTP mount with the familiar Forgejo-style
// paths (https://git.<domain>/<project>/<repo>.git).
func TestGitHostRewritesToGitMount(t *testing.T) {
	var gotPath string
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { gotPath = r.URL.Path })
	h := gitHostRewrite("git.example.test", inner)

	for _, tc := range []struct{ host, path, want string }{
		{"git.example.test", "/acme/app.git/info/refs", "/git/acme/app.git/info/refs"},
		{"GIT.example.test:443", "/acme/app/git-upload-pack", "/git/acme/app/git-upload-pack"},
		{"git.example.test", "/git/acme/app/info/refs", "/git/acme/app/info/refs"}, // already prefixed
		{"api.example.test", "/acme/app.git/info/refs", "/acme/app.git/info/refs"}, // other hosts untouched
	} {
		req := httptest.NewRequest(http.MethodGet, tc.path, nil)
		req.Host = tc.host
		h.ServeHTTP(httptest.NewRecorder(), req)
		if gotPath != tc.want {
			t.Errorf("host %s path %s → %s, want %s", tc.host, tc.path, gotPath, tc.want)
		}
	}

	// No host configured: identity.
	h = gitHostRewrite("", inner)
	req := httptest.NewRequest(http.MethodGet, "/acme/app.git/info/refs", nil)
	req.Host = "git.example.test"
	h.ServeHTTP(httptest.NewRecorder(), req)
	if gotPath != "/acme/app.git/info/refs" {
		t.Errorf("unconfigured host rewrote: %s", gotPath)
	}
}

func TestMetaExposesGitPublicURL(t *testing.T) {
	t.Setenv("QUILL_GIT_PUBLIC_URL", "https://git.example.test/")
	srv := newTestServer(t)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/meta", nil))
	var body struct {
		Git struct {
			PublicURL string `json:"publicUrl"`
		} `json:"git"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Git.PublicURL != "https://git.example.test" {
		t.Fatalf("meta git.publicUrl = %q", body.Git.PublicURL)
	}
}
