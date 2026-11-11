package broker_test

import (
	"bytes"
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

	"github.com/nielsuitterdijk22/quill/internal/broker"
)

// spyOrchestrator records EnsureWorker calls.
type spyOrchestrator struct {
	mu      sync.Mutex
	calls   int
	id      string
	created bool
}

func (s *spyOrchestrator) EnsureWorker(context.Context) (string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	return s.id, s.created, nil
}

func sign(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

func TestWakeRequiresValidSignature(t *testing.T) {
	sp := &spyOrchestrator{id: "i-1"}
	srv := broker.NewServer(broker.Config{Secret: "s", Orchestrator: sp})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// Unsigned.
	resp, err := http.Post(ts.URL+"/wake", "application/json", bytes.NewReader([]byte("{}")))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unsigned wake: %d", resp.StatusCode)
	}

	// Signed with the wrong secret.
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/wake", bytes.NewReader([]byte("{}")))
	req.Header.Set("X-Quill-Dispatch-Signature", sign("wrong", []byte("{}")))
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("bad-signature wake: %d", resp.StatusCode)
	}

	if sp.calls != 0 {
		t.Fatal("an unauthenticated wake must never reach the orchestrator")
	}

	// Health is open.
	resp, err = http.Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("healthz: %d", resp.StatusCode)
	}
}

func TestWakeEnsuresWorker(t *testing.T) {
	sp := &spyOrchestrator{id: "i-1", created: true}
	srv := broker.NewServer(broker.Config{Secret: "s", Orchestrator: sp})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	body := []byte("{}")
	for i := 0; i < 3; i++ { // a wake storm
		req, _ := http.NewRequest(http.MethodPost, ts.URL+"/wake", bytes.NewReader(body))
		req.Header.Set("X-Quill-Dispatch-Signature", sign("s", body))
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		var payload struct {
			InstanceID string `json:"instance_id"`
			Created    bool   `json:"created"`
		}
		_ = json.NewDecoder(io.LimitReader(resp.Body, 4<<10)).Decode(&payload)
		resp.Body.Close()
		if resp.StatusCode != http.StatusAccepted {
			t.Fatalf("wake %d: %d", i, resp.StatusCode)
		}
		if payload.InstanceID != "i-1" || !payload.Created {
			t.Fatalf("wake %d payload: %+v", i, payload)
		}
	}
	// Every wake calls EnsureWorker; idempotence (one instance max) is the
	// orchestrator's list-before-create contract.
	if sp.calls != 3 {
		t.Fatalf("expected 3 orchestrator calls, got %d", sp.calls)
	}
}

func TestBrokerWithoutSecretRefusesWake(t *testing.T) {
	srv := broker.NewServer(broker.Config{Secret: "", Orchestrator: &spyOrchestrator{}})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	body := []byte("{}")
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/wake", bytes.NewReader(body))
	req.Header.Set("X-Quill-Dispatch-Signature", sign("anything", body))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("unconfigured broker: %d", resp.StatusCode)
	}
}
