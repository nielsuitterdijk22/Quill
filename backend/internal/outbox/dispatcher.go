// Package outbox is the reusable transactional-outbox delivery engine shared by
// every Quill→Tempo push. A mutation (or webhook handler) writes a row to some
// outbox table in the same transaction as its state change; a Dispatcher polls
// that table and delivers the row's JSON payload to a Tempo endpoint with retry
// and exponential backoff, surviving Tempo downtime and Quill restarts.
//
// The engine is event-type agnostic: it works against the small Store interface
// over generic PendingEvent rows, so each concrete outbox (project mirror,
// work-item refs, and the next one) supplies a thin adapter mapping its sqlc
// queries onto Store rather than re-implementing the poll/backoff/POST loop.
package outbox

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
)

// TokenSource supplies the bearer token used to authenticate a push to Tempo. It
// is an interface so the static token used for local dev (QUILL_TEMPO_SYNC_TOKEN)
// and the Zitadel client-credentials machine token share one seam.
type TokenSource interface {
	// Token returns the bearer token to present, or an empty string to send the
	// request unauthenticated (local dev).
	Token(ctx context.Context) (string, error)
}

// StaticTokenSource returns a fixed bearer token. Empty means "no auth header".
type StaticTokenSource string

// Token implements TokenSource.
func (t StaticTokenSource) Token(context.Context) (string, error) { return string(t), nil }

// PendingEvent is one undelivered outbox row, reduced to what the dispatcher
// needs: an id to mark on, the JSON body to POST, and how many delivery attempts
// have already been made (for backoff).
type PendingEvent struct {
	ID       uuid.UUID
	Payload  []byte
	Attempts int32
}

// Store is the persistence surface the dispatcher drives. Each concrete outbox
// provides an adapter mapping its generated sqlc queries onto these three calls.
type Store interface {
	// ListPending returns up to limit rows that are due for delivery.
	ListPending(ctx context.Context, limit int32) ([]PendingEvent, error)
	// MarkDelivered records a row as successfully delivered.
	MarkDelivered(ctx context.Context, id uuid.UUID) error
	// MarkFailed reschedules a row for a later retry.
	MarkFailed(ctx context.Context, id uuid.UUID, nextAttemptAt time.Time) error
}

// Default dispatcher tuning.
const (
	defaultPollInterval = 5 * time.Second
	defaultBatchSize    = 50
	defaultBaseBackoff  = 5 * time.Second
	defaultMaxBackoff   = 10 * time.Minute
	defaultHTTPTimeout  = 15 * time.Second
)

// Config configures a Dispatcher. URL and Name are the only per-outbox values;
// the tuning knobs fall back to sensible defaults.
type Config struct {
	// Name identifies this outbox in log lines (e.g. "project sync"). Also used in
	// the "disabled" startup message.
	Name string
	// URL is the Tempo endpoint this outbox delivers to. Empty disables the
	// outbox entirely: the dispatcher stays idle (callers must also skip enqueue).
	URL string
	// PollInterval is how often the outbox is polled for pending rows.
	PollInterval time.Duration
	// BatchSize caps how many pending rows are pulled per poll.
	BatchSize int32
	// BaseBackoff is the delay before the first retry; it doubles each attempt.
	BaseBackoff time.Duration
	// MaxBackoff caps the exponential backoff.
	MaxBackoff time.Duration
	// HTTPTimeout bounds a single delivery request.
	HTTPTimeout time.Duration
}

func (c Config) withDefaults() Config {
	if c.Name == "" {
		c.Name = "outbox"
	}
	if c.PollInterval <= 0 {
		c.PollInterval = defaultPollInterval
	}
	if c.BatchSize <= 0 {
		c.BatchSize = defaultBatchSize
	}
	if c.BaseBackoff <= 0 {
		c.BaseBackoff = defaultBaseBackoff
	}
	if c.MaxBackoff <= 0 {
		c.MaxBackoff = defaultMaxBackoff
	}
	if c.HTTPTimeout <= 0 {
		c.HTTPTimeout = defaultHTTPTimeout
	}
	return c
}

// Dispatcher polls an outbox Store and delivers undelivered rows to Tempo with
// retry and exponential backoff.
type Dispatcher struct {
	cfg    Config
	store  Store
	tokens TokenSource
	client *http.Client
	logger *slog.Logger
	// now is overridable in tests to exercise backoff scheduling deterministically.
	now func() time.Time
}

// NewDispatcher builds a Dispatcher. tokens may be nil (treated as no auth);
// logger may be nil.
func NewDispatcher(cfg Config, store Store, tokens TokenSource, logger *slog.Logger) *Dispatcher {
	cfg = cfg.withDefaults()
	if logger == nil {
		logger = slog.Default()
	}
	if tokens == nil {
		tokens = StaticTokenSource("")
	}
	return &Dispatcher{
		cfg:    cfg,
		store:  store,
		tokens: tokens,
		client: &http.Client{Timeout: cfg.HTTPTimeout},
		logger: logger,
		now:    func() time.Time { return time.Now().UTC() },
	}
}

// Enabled reports whether this outbox is configured. When false the dispatcher
// is idle.
func (d *Dispatcher) Enabled() bool { return d.cfg.URL != "" }

// Run polls the outbox until ctx is cancelled. It is a no-op when the outbox is
// disabled (URL empty), so callers can start it unconditionally.
func (d *Dispatcher) Run(ctx context.Context) {
	if !d.Enabled() {
		d.logger.Info(d.cfg.Name + " disabled (endpoint URL empty); dispatcher idle")
		return
	}
	d.logger.Info(d.cfg.Name+" dispatcher started", "url", d.cfg.URL, "pollInterval", d.cfg.PollInterval)
	ticker := time.NewTicker(d.cfg.PollInterval)
	defer ticker.Stop()

	// Drain any backlog immediately on startup rather than waiting a full tick.
	if _, err := d.ProcessBatch(ctx); err != nil && ctx.Err() == nil {
		d.logger.Warn(d.cfg.Name+" batch failed", "error", err)
	}
	for {
		select {
		case <-ctx.Done():
			d.logger.Info(d.cfg.Name + " dispatcher stopped")
			return
		case <-ticker.C:
			if _, err := d.ProcessBatch(ctx); err != nil && ctx.Err() == nil {
				d.logger.Warn(d.cfg.Name+" batch failed", "error", err)
			}
		}
	}
}

// ProcessBatch pulls one batch of due rows and attempts to deliver each, marking
// delivered rows and rescheduling failed ones with backoff. It returns the
// number of rows successfully delivered. Exported so it can be driven directly
// in tests.
func (d *Dispatcher) ProcessBatch(ctx context.Context) (int, error) {
	events, err := d.store.ListPending(ctx, d.cfg.BatchSize)
	if err != nil {
		return 0, fmt.Errorf("list pending events: %w", err)
	}
	delivered := 0
	for _, ev := range events {
		if err := d.deliver(ctx, ev.Payload); err != nil {
			next := d.now().Add(d.backoff(ev.Attempts))
			if merr := d.store.MarkFailed(ctx, ev.ID, next); merr != nil {
				return delivered, fmt.Errorf("mark event failed: %w", merr)
			}
			d.logger.Warn(d.cfg.Name+" delivery failed; will retry",
				"eventId", ev.ID, "attempts", ev.Attempts+1, "nextAttemptAt", next, "error", err)
			continue
		}
		if err := d.store.MarkDelivered(ctx, ev.ID); err != nil {
			return delivered, fmt.Errorf("mark event delivered: %w", err)
		}
		delivered++
	}
	return delivered, nil
}

// backoff returns the delay before the next attempt given the number of attempts
// already made: base * 2^attempts, capped at MaxBackoff.
func (d *Dispatcher) backoff(attempts int32) time.Duration {
	backoff := d.cfg.BaseBackoff
	for i := int32(0); i < attempts; i++ {
		backoff *= 2
		if backoff >= d.cfg.MaxBackoff {
			return d.cfg.MaxBackoff
		}
	}
	if backoff > d.cfg.MaxBackoff {
		return d.cfg.MaxBackoff
	}
	return backoff
}

// deliver POSTs a single payload to Tempo. A non-2xx response or any transport
// error (Tempo down) is returned as an error so the caller reschedules.
func (d *Dispatcher) deliver(ctx context.Context, payload []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.cfg.URL, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	token, err := d.tokens.Token(ctx)
	if err != nil {
		return fmt.Errorf("acquire token: %w", err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := d.client.Do(req)
	if err != nil {
		return fmt.Errorf("post event: %w", err)
	}
	defer resp.Body.Close()
	// Drain so the connection can be reused.
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("tempo %s returned status %d", d.cfg.Name, resp.StatusCode)
	}
	return nil
}
