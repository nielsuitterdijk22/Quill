// Package broker is the CI control plane of the serverless epic (PR A2): a
// min_scale-0 Serverless Container that receives wake-ups from the api (one
// per enqueue) and from the cron sweeper, and ensures exactly one ephemeral CI
// instance is running per burst. The ephemeral instance runs ciworker, which
// drains the queue and self-destructs — the broker never executes jobs.
package broker

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

// Orchestrator owns the compute the CI jobs run on. The Scaleway
// implementation boots/destroys ephemeral DEV1-S instances; tests stub it.
type Orchestrator interface {
	// EnsureWorker makes sure a CI worker instance exists and is (or is
	// starting). It returns the instance id and whether a new instance was
	// created. It must be idempotent: a wake storm must not boot a fleet.
	EnsureWorker(ctx context.Context) (instanceID string, created bool, err error)
}

// Config wires the broker.
type Config struct {
	// Secret is QUILL_PIPELINE_DISPATCH_SECRET: the same shared secret the
	// api uses to sign wake-ups and the ciworker uses to claim jobs.
	Secret string
	// Orchestrator boots/destroys the ephemeral CI instance.
	Orchestrator Orchestrator
	// Logger is required.
	Logger *slog.Logger
}

// Server is the broker's HTTP surface.
type Server struct {
	cfg      Config
	mu       sync.Mutex // single-flights EnsureWorker for concurrent wakes
	lastBoot time.Time
	lastID   string
}

// NewServer builds the broker.
func NewServer(cfg Config) *Server {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &Server{cfg: cfg}
}

// Handler returns the broker's routes.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/wake", s.handleWake)
	return mux
}

// handleWake authenticates a wake-up (HMAC over the raw body — empty is fine)
// and ensures a worker instance exists. Wakes are idempotent and cheap: the
// orchestrator's EnsureWorker lists before it creates.
func (s *Server) handleWake(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	secret := s.cfg.Secret
	if secret == "" {
		http.Error(w, "broker unconfigured (no dispatch secret)", http.StatusServiceUnavailable)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 64<<10))
	if err != nil {
		http.Error(w, "read body", http.StatusBadRequest)
		return
	}
	got := r.Header.Get("X-Quill-Dispatch-Signature")
	want := dispatchSignature(secret, body)
	if got == "" || !hmac.Equal([]byte(want), []byte(got)) {
		http.Error(w, "invalid or missing dispatch signature", http.StatusUnauthorized)
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	// Detached from the request: the api's wake is fire-and-forget with a
	// short timeout, and a hang-up must never abort create → user-data →
	// power-on halfway (that strands a powered-off instance).
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 2*time.Minute)
	defer cancel()
	id, created, err := s.cfg.Orchestrator.EnsureWorker(ctx)
	if err != nil {
		s.cfg.Logger.Error("ensure worker failed", "error", err)
		http.Error(w, "ensure worker failed", http.StatusInternalServerError)
		return
	}
	if created {
		s.lastBoot = time.Now()
		s.lastID = id
	}
	s.cfg.Logger.Info("wake", "instance", id, "created", created)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]any{"instance_id": id, "created": created})
}

// dispatchSignature mirrors the api/ciworker HMAC scheme.
func dispatchSignature(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}
