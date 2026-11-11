package ciworker_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nielsuitterdijk22/quill/internal/ciworker"
	"github.com/nielsuitterdijk22/quill/internal/pipeline"
)

// fakeQueue is an in-memory stand-in for the api's dispatch endpoints. It
// validates the HMAC on every request so the loop's signing is under test.
type fakeQueue struct {
	mu         sync.Mutex
	secret     string
	job        *ciworker.ClaimedJob
	claims     []string
	heartbeats []string
	reports    []map[string]any
}

func (f *fakeQueue) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	body, _ := io.ReadAll(r.Body)
	if r.URL.Path == "/api/v1/internal/dispatch/claim" {
		f.claims = append(f.claims, "claim")
	}
	if sig := r.Header.Get("X-Quill-Dispatch-Signature"); sig == "" || !f.validSig(body, sig) {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	switch r.URL.Path {
	case "/api/v1/internal/dispatch/claim":
		if f.job == nil {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		job := f.job
		f.job = nil // claim consumes the job, like the real queue
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"queue_id": job.QueueID.String(),
			"run_id":   job.RunID.String(),
			"attempts": job.Attempts,
			"spec":     map[string]any{"WorkflowYAML": "name: t\n"},
		})
	case "/api/v1/internal/dispatch/heartbeat":
		f.heartbeats = append(f.heartbeats, "hb")
		w.WriteHeader(http.StatusNoContent)
	case "/api/v1/internal/dispatch/report":
		var in map[string]any
		_ = json.Unmarshal(body, &in)
		f.reports = append(f.reports, in)
		w.WriteHeader(http.StatusNoContent)
	default:
		w.WriteHeader(http.StatusInternalServerError)
	}
}

func (f *fakeQueue) validSig(body []byte, got string) bool {
	mac := hmac.New(sha256.New, []byte(f.secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil)) == got
}

func TestDrainRunsAndReportsOneJob(t *testing.T) {
	q := &fakeQueue{secret: "s3cret"}
	q.job = &ciworker.ClaimedJob{
		QueueID: uuid.New(), RunID: uuid.New(), Attempts: 1,
	}
	srv := httptest.NewServer(q)
	defer srv.Close()

	runner := &recordingRunner{result: pipeline.RunResult{Status: pipeline.StatusSuccess}}
	terminated := make(chan struct{})
	_, err := ciworker.Drain(context.Background(), ciworker.NewClient(srv.URL, "s3cret"),
		"w1", runner, 50*time.Millisecond,
		func(context.Context) error { close(terminated); return nil },
		func(string, ...any) {})
	if err != nil {
		t.Fatalf("drain: %v", err)
	}
	if len(runner.specs) != 1 || runner.specs[0].WorkflowYAML != "name: t\n" {
		t.Fatalf("runner saw %+v", runner.specs)
	}
	if len(q.reports) != 1 {
		t.Fatalf("expected exactly one report, got %d", len(q.reports))
	}
	if q.reports[0]["error"] != nil {
		t.Fatalf("success path reported an error: %v", q.reports[0])
	}
	if _, ok := q.reports[0]["result"]; !ok {
		t.Fatalf("report is missing the result payload: %+v", q.reports[0])
	}
	select {
	case <-terminated:
	default:
		t.Fatal("terminator not invoked on idle exit")
	}
}

func TestDrainReportsRunnerStartupFailure(t *testing.T) {
	q := &fakeQueue{secret: "s3cret"}
	q.job = &ciworker.ClaimedJob{QueueID: uuid.New(), RunID: uuid.New()}
	srv := httptest.NewServer(q)
	defer srv.Close()

	runner := &failingRunner{}
	if _, err := ciworker.Drain(context.Background(), ciworker.NewClient(srv.URL, "s3cret"),
		"w1", runner, 50*time.Millisecond, nil, func(string, ...any) {}); err != nil {
		t.Fatalf("drain: %v", err)
	}
	if len(q.reports) != 1 || q.reports[0]["error"] == "" {
		t.Fatalf("startup failure should report an error, got %v", q.reports)
	}
}

func TestDrainSignedEverywhere(t *testing.T) {
	// Claim with the wrong secret refuses; drain must exit idle, not loop on 401s.
	q := &fakeQueue{secret: "right"}
	srv := httptest.NewServer(q)
	defer srv.Close()
	done := make(chan struct{})
	go func() {
		_, _ = ciworker.Drain(context.Background(), ciworker.NewClient(srv.URL, "wrong"),
			"w1", &recordingRunner{}, 60*time.Millisecond, nil,
			func(string, ...any) {})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("drain did not exit on persistent claim failures")
	}
	if len(q.claims) == 0 {
		t.Fatal("expected at least one claim attempt")
	}
}

// recordingRunner / failingRunner implement pipeline.Runner for drain tests.
type recordingRunner struct {
	result pipeline.RunResult
	specs  []pipeline.JobSpec
}

func (r *recordingRunner) Run(_ context.Context, spec pipeline.JobSpec) (pipeline.RunResult, error) {
	r.specs = append(r.specs, spec)
	return r.result, nil
}

type failingRunner struct{}

func (failingRunner) Run(context.Context, pipeline.JobSpec) (pipeline.RunResult, error) {
	return pipeline.RunResult{}, context.DeadlineExceeded
}
