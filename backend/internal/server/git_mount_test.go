package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nielsuitterdijk22/quill/internal/config"
	"github.com/nielsuitterdijk22/quill/internal/logging"
)

// The /git mount must hand the smart-HTTP engine prefix-free paths: chi's
// Mount does not strip "/git", so without StripPrefix the engine parsed
// "git" as the owner and 404'd every clone in production (D2 cutover,
// 2026-11-13). An unknown repo asked anonymously must get the Basic
// challenge (401), never the router's "404 page not found" — the same holds
// on the dedicated git host.
func TestGitMountServesEnginePaths(t *testing.T) {
	st := refsTestStore(t)
	t.Setenv("QUILL_GIT_ENGINE_MODE", "own")
	t.Setenv("QUILL_GIT_S3_ENDPOINT", "http://127.0.0.1:1")
	t.Setenv("QUILL_GIT_S3_REGION", "test")
	t.Setenv("QUILL_GIT_S3_BUCKET", "test")
	t.Setenv("QUILL_GIT_S3_ACCESS_KEY", "k")
	t.Setenv("QUILL_GIT_S3_SECRET_KEY", "s")
	t.Setenv("QUILL_GIT_HOST", "git.example.test")
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	srv := New(cfg, logging.New("error", "text"), st)

	for _, tc := range []struct{ host, path string }{
		{"api.example.test", "/git/nope-project/nope-repo.git/info/refs?service=git-upload-pack"},
		{"git.example.test", "/nope-project/nope-repo.git/info/refs?service=git-upload-pack"},
	} {
		req := httptest.NewRequest(http.MethodGet, tc.path, nil)
		req.Host = tc.host
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s%s: status %d (%q), want 401 Basic challenge", tc.host, tc.path, rec.Code, rec.Body.String())
		}
	}
}
