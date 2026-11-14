package pipeline

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
)

type fetchCall struct{ url, token string }

type fakeActionCache struct {
	calls []fetchCall
	have  map[string]bool // url → exists
}

func (f *fakeActionCache) Fetch(_ context.Context, _, url, _, token string) (string, error) {
	f.calls = append(f.calls, fetchCall{url, token})
	if f.have[url] {
		return "sha-" + url, nil
	}
	return "", errors.New("repository not found")
}

func (f *fakeActionCache) GetTarArchive(context.Context, string, string, string) (io.ReadCloser, error) {
	return nil, errors.New("unused")
}

func TestActionSourceGitHubFirstWithoutToken(t *testing.T) {
	ctx := context.Background()
	quill := "https://git.example.test"

	// actions/setup-go: on GitHub, fetched anonymously; Quill never asked.
	f := &fakeActionCache{have: map[string]bool{"https://github.com/actions/setup-go": true}}
	src := newActionSource(f, quill+"/")
	sha, err := src.Fetch(ctx, "actions/setup-go", quill+"/actions/setup-go", "v5", "quill-run-token")
	if err != nil || sha != "sha-https://github.com/actions/setup-go" {
		t.Fatalf("fetch: %q %v", sha, err)
	}
	if len(f.calls) != 1 || f.calls[0].token != "" {
		t.Fatalf("calls = %+v (the Quill token must never go to github.com)", f.calls)
	}

	// A Quill-hosted action: GitHub misses, Quill answers with the run token.
	f = &fakeActionCache{have: map[string]bool{quill + "/team/deploy": true}}
	src = newActionSource(f, quill)
	if _, err := src.Fetch(ctx, "team/deploy", quill+"/team/deploy", "v1", "quill-run-token"); err != nil {
		t.Fatal(err)
	}
	if len(f.calls) != 2 || f.calls[0].token != "" || f.calls[1] != (fetchCall{quill + "/team/deploy", "quill-run-token"}) {
		t.Fatalf("calls = %+v", f.calls)
	}

	// Neither has it: the error names both.
	f = &fakeActionCache{}
	src = newActionSource(f, quill)
	_, err = src.Fetch(ctx, "nope/missing", quill+"/nope/missing", "v1", "t")
	if err == nil || !strings.Contains(err.Error(), "github.com") || !strings.Contains(err.Error(), "Quill") {
		t.Fatalf("err = %v", err)
	}

	// Any other URL (explicit github.com replacement, no Quill host) is untouched.
	f = &fakeActionCache{have: map[string]bool{"https://github.com/x/y": true}}
	src = newActionSource(f, quill)
	if _, err := src.Fetch(ctx, "x/y", "https://github.com/x/y", "v1", "gh"); err != nil || len(f.calls) != 1 || f.calls[0].token != "gh" {
		t.Fatalf("pass-through: %v %+v", err, f.calls)
	}
}

func TestExplainFailureSurfacesExecutorError(t *testing.T) {
	res := RunResult{Jobs: []JobResult{
		{Key: "build", Status: StatusFailure, Steps: []StepResult{
			{Name: "setup", Status: StatusSuccess, Logs: "✅  Success - Set up job"},
			{Name: "actions/setup-go@v5", Status: StatusSkipped},
		}},
		{Key: "lint", Status: StatusFailure, Steps: []StepResult{{Name: "go vet", Status: StatusFailure}}},
		{Key: "ok", Status: StatusSuccess, Steps: []StepResult{{Name: "echo", Status: StatusSuccess}}},
	}}
	var live []string
	explainFailure(&res, errors.New(`failed to fetch "actions/setup-go"`), func(job, step, line string) {
		live = append(live, job+"/"+step+": "+line)
	})
	b := res.Jobs[0]
	if b.Steps[0].Status != StatusFailure || !strings.HasSuffix(b.Steps[0].Logs, "Set up job\nError: failed to fetch \"actions/setup-go\"\n") {
		t.Fatalf("build setup = %+v", b.Steps[0])
	}
	l := res.Jobs[1]
	if len(l.Steps) != 2 || l.Steps[0].Name != "setup" || l.Steps[0].Status == StatusFailure || !strings.Contains(l.Steps[0].Logs, "Error:") {
		t.Fatalf("lint steps = %+v (a step ran, so setup is context, not the failure)", l.Steps)
	}
	if len(res.Jobs[2].Steps) != 1 {
		t.Fatal("successful job touched")
	}
	if len(live) != 2 || !strings.HasPrefix(live[0], "build/setup: Error:") {
		t.Fatalf("live = %q", live)
	}
}
