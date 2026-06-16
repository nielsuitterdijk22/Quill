package dispatch

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nielsuitterdijk22/quill/internal/pipeline"
)

type runnerFunc func(context.Context, pipeline.JobSpec) (pipeline.RunResult, error)

func (f runnerFunc) Run(ctx context.Context, spec pipeline.JobSpec) (pipeline.RunResult, error) {
	return f(ctx, spec)
}

func TestDispatchRunRequiresValidSignature(t *testing.T) {
	srv := New(nil, runnerFunc(func(context.Context, pipeline.JobSpec) (pipeline.RunResult, error) {
		t.Fatal("runner should not be called")
		return pipeline.RunResult{}, nil
	}), "secret")

	req := httptest.NewRequest(http.MethodPost, "/api/v1/runs", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestHTTPRunnerDispatchesSpec(t *testing.T) {
	want := pipeline.JobSpec{WorkflowYAML: "on: push\njobs: {}\n", WorkflowPath: ".github/workflows/ci.yml", Event: "push", CloneURL: "http://forgejo/acme/widget.git", CloneAuthHeader: "Authorization: token secret"}
	result := pipeline.RunResult{Status: pipeline.StatusSuccess}

	srv := New(nil, runnerFunc(func(_ context.Context, got pipeline.JobSpec) (pipeline.RunResult, error) {
		if got.WorkflowPath != want.WorkflowPath || got.Event != want.Event || got.CloneURL != want.CloneURL || got.CloneAuthHeader != want.CloneAuthHeader {
			t.Fatalf("spec = %+v, want %+v", got, want)
		}
		return result, nil
	}), "secret")
	ts := httptest.NewServer(srv)
	defer ts.Close()

	got, err := pipeline.NewHTTPRunner(ts.URL, "secret").Run(context.Background(), want)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got.Status != pipeline.StatusSuccess {
		b, _ := json.Marshal(got)
		t.Fatalf("result = %s, want success", b)
	}
}
