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

// TestCloudInitIsolatesTheMachine (2026-11-14, replaces
// TestCloudInitCarriesTerminationCredsAndBlocksMetadata): jobs on a CI
// instance can become root (they get the Docker socket), so the instance
// must carry only its own worker token — no dispatch secret, no cloud key —
// and lose the metadata service before the worker starts.
func TestCloudInitIsolatesTheMachine(t *testing.T) {
	ci := broker.CloudInit(broker.CloudInitOpts{
		APIURL: "https://api.example", WorkerToken: "qcw1.payload.sig", WorkerImage: "rg/img:1",
		Zone: "fr-par-1", InstanceTag: "t",
		RegistryUser: "nologin", RegistryPassword: "pull-only",
		UbuntuImage: "node:22-bookworm",
	})
	for _, want := range []string{
		"QUILL_CIW_TOKEN=qcw1.payload.sig",
		"QUILL_CIW_SELF_TERMINATE=true",
		"QUILL_PIPELINE_UBUNTU_IMAGE=node:22-bookworm",
		// user workflow containers must not read user-data off the metadata
		// service: bridge-networked ones (DOCKER-USER) and host-networked
		// ones, act's default (OUTPUT)
		"DOCKER-USER -d 169.254.42.42", // Scaleway's metadata service
		"DOCKER-USER -d 169.254.169.254",
		"OUTPUT -d 169.254.42.42",
		"OUTPUT -d 169.254.169.254",
		// the backend image runs unprivileged; the worker needs the socket
		"-u 0",
		"/quill-ciworker",
	} {
		if !strings.Contains(ci, want) {
			t.Errorf("cloud-init missing %q:\n%s", want, ci)
		}
	}
	for _, forbidden := range []string{
		"SCW_ACCESS_KEY", "SCW_SECRET_KEY", "QUILL_PIPELINE_DISPATCH_SECRET", "QUILL_CIW_WORKER_ID",
	} {
		if strings.Contains(ci, forbidden) {
			t.Errorf("cloud-init must not carry %s:\n%s", forbidden, ci)
		}
	}
	// The metadata block must be in place before the worker starts.
	if strings.Index(ci, "OUTPUT -d 169.254.42.42") > strings.Index(ci, "docker run") ||
		strings.Index(ci, "169.254.42.42") > strings.Index(ci, "docker run") {
		t.Error("metadata block must precede the worker run")
	}
}
