package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseQuillRemote(t *testing.T) {
	for in, want := range map[string]string{
		"https://git.black-tulip.nl/nuitterdijk/tempo.git":     "nuitterdijk/tempo",
		"https://git.black-tulip.nl/nuitterdijk/quill":         "nuitterdijk/quill",
		"https://x:tok@git.example.com/acme/app.git":           "acme/app",
		"https://api.black-tulip.nl/git/nuitterdijk/quill.git": "nuitterdijk/quill",
		"https://github.com/nielsuitterdijk22/Quill.git":       "",
		"git@github.com:nielsuitterdijk22/Quill.git":           "",
	} {
		got, ok := parseQuillRemote(in)
		if (want == "") == ok || got != want {
			t.Errorf("parseQuillRemote(%q) = %q, %v; want %q", in, got, ok, want)
		}
	}
}

// The client sends the API token as a bearer, hits the repo-scoped paths
// with the right methods and bodies, and surfaces API errors.
func TestClientCommands(t *testing.T) {
	type hit struct{ method, path, auth, body string }
	var hits []hit
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var b bytes.Buffer
		b.ReadFrom(r.Body)
		hits = append(hits, hit{r.Method, r.URL.RequestURI(), r.Header.Get("Authorization"), b.String()})
		switch {
		case strings.HasSuffix(r.URL.Path, "/pulls/9/merge"):
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte(`{"message":"blocked by branch policy"}`))
			return
		case strings.Contains(r.URL.Path, "/pipelines"):
			w.Write([]byte(`{"run":{"runNumber":4,"status":"success","workflowPath":".github/workflows/d.yml"}}`))
			return
		}
		w.Write([]byte(`{"pull":{"number":8,"title":"t","state":"closed","head":{"ref":"f"},"base":{"ref":"main"}}}`))
	}))
	defer srv.Close()
	tok := filepath.Join(t.TempDir(), "token")
	os.WriteFile(tok, []byte("qat_test\n"), 0o600)
	common := []string{"--api", srv.URL, "--repo", "acme/app", "--token-file", tok}

	if err := runPR(append([]string{"close", "8"}, common...)); err != nil {
		t.Fatalf("close: %v", err)
	}
	if err := runPR(append([]string{"merge", "7", "--method", "rebase"}, common...)); err != nil {
		t.Fatalf("merge: %v", err)
	}
	err := runPR(append(append([]string{"merge"}, common...), "9"))
	if err == nil || !strings.Contains(err.Error(), "blocked by branch policy") {
		t.Fatalf("API error not surfaced: %v", err)
	}
	if err := runRun(append([]string{"trigger", "--workflow", ".github/workflows/d.yml", "--ref", "main"}, common...)); err != nil {
		t.Fatalf("trigger: %v", err)
	}
	if err := runRun(append(append([]string{"watch", "--workflow", ".github/workflows/d.yml"}, common...), "4")); err != nil {
		t.Fatalf("watch: %v", err)
	}

	want := []struct{ method, path, body string }{
		{"PATCH", "/api/v1/projects/acme/repos/app/pulls/8", `{"state":"closed"}`},
		{"POST", "/api/v1/projects/acme/repos/app/pulls/7/merge", `{"method":"rebase"}`},
		{"POST", "/api/v1/projects/acme/repos/app/pulls/9/merge", `{"method":"merge"}`},
		{"POST", "/api/v1/projects/acme/repos/app/pipelines", ""},
		{"GET", "/api/v1/projects/acme/repos/app/pipelines/runs/4?workflow=.github%2Fworkflows%2Fd.yml", ""},
	}
	if len(hits) != len(want) {
		t.Fatalf("requests = %+v", hits)
	}
	for i, w := range want {
		h := hits[i]
		if h.method != w.method || h.path != w.path || h.auth != "Bearer qat_test" {
			t.Errorf("request %d = %s %s (%s), want %s %s", i, h.method, h.path, h.auth, w.method, w.path)
		}
		if w.body != "" && h.body != w.body {
			t.Errorf("request %d body = %s, want %s", i, h.body, w.body)
		}
	}
	var trig map[string]string
	json.Unmarshal([]byte(hits[3].body), &trig)
	if trig["workflow"] != ".github/workflows/d.yml" || trig["ref"] != "main" {
		t.Errorf("trigger body = %s", hits[3].body)
	}
}
