package broker_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nielsuitterdijk22/quill/internal/broker"
)

// ctxOrchestrator reports whether the context EnsureWorker received was
// cancelled while it worked (a slow Scaleway create racing a client hang-up).
type ctxOrchestrator struct {
	cancelled chan bool
}

func (c *ctxOrchestrator) EnsureWorker(ctx context.Context) (string, bool, error) {
	select {
	case <-ctx.Done():
		c.cancelled <- true
		return "", false, ctx.Err()
	case <-time.After(300 * time.Millisecond):
		c.cancelled <- false
		return "i-1", true, nil
	}
}

// A caller that gives up (the api's fire-and-forget wake times out long
// before a cold broker finishes create → user-data → power-on) must not abort
// the orchestration halfway: a created-but-never-powered instance is a leak.
func TestWakeOrchestrationSurvivesClientHangup(t *testing.T) {
	orch := &ctxOrchestrator{cancelled: make(chan bool, 1)}
	srv := broker.NewServer(broker.Config{Secret: "s", Orchestrator: orch})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	body := []byte("{}")
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, ts.URL+"/wake", bytes.NewReader(body))
	req.Header.Set("X-Quill-Dispatch-Signature", sign("s", body))
	if resp, err := http.DefaultClient.Do(req); err == nil {
		resp.Body.Close()
	}
	select {
	case wasCancelled := <-orch.cancelled:
		if wasCancelled {
			t.Fatal("client hang-up cancelled the orchestration mid-flight")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("orchestrator never finished")
	}
}

func TestCloudInitCarriesTerminationCredsAndBlocksMetadata(t *testing.T) {
	ci := broker.CloudInit(broker.CloudInitOpts{
		APIURL: "https://api.example", DispatchSecret: "d", WorkerImage: "rg/img:1",
		Zone: "fr-par-1", InstanceTag: "t",
		SCWAccessKey: "SCWAK", SCWSecretKey: "sk-123", SCWProjectID: "proj",
		UbuntuImage: "node:22-bookworm",
	})
	for _, want := range []string{
		"SCW_ACCESS_KEY=SCWAK",
		"SCW_SECRET_KEY=sk-123",
		"SCW_DEFAULT_PROJECT_ID=proj",
		"QUILL_PIPELINE_UBUNTU_IMAGE=node:22-bookworm",
		// user workflow containers must not read user-data (secrets) off the
		// metadata service
		"DOCKER-USER -d 169.254.169.254",
		// the backend image runs unprivileged; the worker needs the socket
		"-u 0",
		"/quill-ciworker",
	} {
		if !strings.Contains(ci, want) {
			t.Errorf("cloud-init missing %q:\n%s", want, ci)
		}
	}
	// The metadata block must be in place before the worker starts.
	if strings.Index(ci, "169.254.169.254") > strings.Index(ci, "docker run") {
		t.Error("metadata block must precede the worker run")
	}
}
