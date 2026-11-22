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

// `run cancel` posts to the run's cancel route, scoped to its workflow, and
// `run view` reports queue wait, run time and the jobs/steps of a run.
func TestRunCancelAndView(t *testing.T) {
	var cancelled string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			cancelled = r.URL.RequestURI()
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.Write([]byte(`{"run":{"runNumber":47,"status":"running","workflowPath":".github/workflows/f.yml",` +
			`"event":"pull_request","ref":"refs/heads/x","commitSha":"c33f637aa","createdAt":"2026-11-21T12:00:00Z","startedAt":"2026-11-21T12:28:00Z",` +
			`"jobs":[{"name":"check","status":"running","startedAt":"2026-11-21T12:28:01Z","steps":[` +
			`{"name":"build","status":"success","logs":"built ok","startedAt":"2026-11-21T12:28:02Z","finishedAt":"2026-11-21T12:29:02Z"},` +
			`{"name":"test","status":"running","logs":"still going","startedAt":"2026-11-21T12:29:02Z"}]}]}}`))
	}))
	defer srv.Close()
	tok := filepath.Join(t.TempDir(), "token")
	os.WriteFile(tok, []byte("qat_test\n"), 0o600)
	common := []string{"--api", srv.URL, "--repo", "acme/app", "--token-file", tok, "--workflow", ".github/workflows/f.yml"}

	if err := runRun(append([]string{"cancel", "47"}, common...)); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if want := "/api/v1/projects/acme/repos/app/pipelines/runs/47/cancel?workflow=.github%2Fworkflows%2Ff.yml"; cancelled != want {
		t.Errorf("cancel hit %q, want %q", cancelled, want)
	}
	if err := runRun([]string{"cancel", "47", "--api", srv.URL, "--repo", "acme/app", "--token-file", tok}); err == nil {
		t.Error("cancel without --workflow must fail: run numbers count per workflow")
	}
}

func TestSpan(t *testing.T) {
	if got := span("2026-11-21T12:00:00Z", "2026-11-21T12:28:00Z"); got != "28m0s" {
		t.Errorf("span = %q", got)
	}
	if got := span("", "2026-11-21T12:28:00Z"); got != "" {
		t.Errorf("missing start must render empty, got %q", got)
	}
	if got := span("2026-11-21T12:00:00Z", ""); got == "" {
		t.Error("an open span runs to now")
	}
}
