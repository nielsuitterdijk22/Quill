package workitemrefs

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/nielsuitterdijk22/quill/internal/projectsync"
	"github.com/nielsuitterdijk22/quill/internal/store/db"
)

// TokenSource is the auth seam shared with the project-mirror dispatcher: the
// static QUILL_TEMPO_SYNC_TOKEN used today can be swapped for the Zitadel
// client-credentials machine token (PR 8.1) without touching this package. It is
// aliased from internal/projectsync so both outbound pushes present their bearer
// token through the same interface.
type TokenSource = projectsync.TokenSource

// StaticTokenSource returns a fixed bearer token; empty means "no auth header".
type StaticTokenSource = projectsync.StaticTokenSource

// Outbox is the store surface the dispatcher needs. *store.Store satisfies it
// via its embedded *db.Queries; tests supply an in-memory fake.
type Outbox interface {
	ListPendingWorkItemRefEvents(ctx context.Context, limit int32) ([]db.WorkItemRefOutbox, error)
	MarkWorkItemRefEventDelivered(ctx context.Context, id uuid.UUID) error
	MarkWorkItemRefEventFailed(ctx context.Context, arg db.MarkWorkItemRefEventFailedParams) error
}

// Default dispatcher tuning, matching the project-mirror dispatcher.
const (
	defaultPollInterval = 5 * time.Second
	defaultBatchSize    = 50
	defaultBaseBackoff  = 5 * time.Second
	defaultMaxBackoff   = 10 * time.Minute
	defaultHTTPTimeout  = 15 * time.Second
)

// Config configures a Dispatcher. Only URL is required; the rest fall back to
// sensible defaults.
type Config struct {
	// URL is Tempo's work-item-refs endpoint. Empty disables the feature entirely:
	// the dispatcher stays idle and the webhook handler skips enqueueing.
	URL string
	// PollInterval is how often the outbox is polled for pending pushes.
	PollInterval time.Duration
	// BatchSize caps how many pending pushes are pulled per poll.
	BatchSize int32
	// BaseBackoff is the delay before the first retry; it doubles each attempt.
	BaseBackoff time.Duration
	// MaxBackoff caps the exponential backoff.
	MaxBackoff time.Duration
	// HTTPTimeout bounds a single delivery request.
	HTTPTimeout time.Duration
}

func (c Config) withDefaults() Config {
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

// Dispatcher polls the work-item-ref outbox and delivers undelivered pushes to
// Tempo with retry and exponential backoff. It is the reliable half of the
// outbox: the webhook handler writes a row; the dispatcher makes sure it reaches
// Tempo eventually, even across Tempo or Quill restarts.
type Dispatcher struct {
	cfg    Config
	outbox Outbox
	tokens TokenSource
	client *http.Client
	logger *slog.Logger
	// now is overridable in tests to exercise backoff scheduling deterministically.
	now func() time.Time
}

// NewDispatcher builds a Dispatcher. tokens may be nil (treated as no auth);
// logger may be nil.
func NewDispatcher(cfg Config, outbox Outbox, tokens TokenSource, logger *slog.Logger) *Dispatcher {
	cfg = cfg.withDefaults()
	if logger == nil {
		logger = slog.Default()
	}
	if tokens == nil {
		tokens = StaticTokenSource("")
	}
	return &Dispatcher{
		cfg:    cfg,
		outbox: outbox,
		tokens: tokens,
		client: &http.Client{Timeout: cfg.HTTPTimeout},
		logger: logger,
		now:    func() time.Time { return time.Now().UTC() },
	}
}

// Enabled reports whether the feature is configured. When false the dispatcher is
// idle and the webhook handler must not enqueue.
func (d *Dispatcher) Enabled() bool { return d.cfg.URL != "" }

// Run polls the outbox until ctx is cancelled. It is a no-op when the feature is
// disabled (URL empty), so callers can start it unconditionally.
func (d *Dispatcher) Run(ctx context.Context) {
	if !d.Enabled() {
		d.logger.Info("work-item ref sync disabled (QUILL_TEMPO_SYNC_REFS_URL empty); dispatcher idle")
		return
	}
	d.logger.Info("work-item ref dispatcher started", "url", d.cfg.URL, "pollInterval", d.cfg.PollInterval)
	ticker := time.NewTicker(d.cfg.PollInterval)
	defer ticker.Stop()

	// Drain any backlog immediately on startup rather than waiting a full tick.
	if _, err := d.ProcessBatch(ctx); err != nil && ctx.Err() == nil {
		d.logger.Warn("work-item ref batch failed", "error", err)
	}
	for {
		select {
		case <-ctx.Done():
			d.logger.Info("work-item ref dispatcher stopped")
			return
		case <-ticker.C:
			if _, err := d.ProcessBatch(ctx); err != nil && ctx.Err() == nil {
				d.logger.Warn("work-item ref batch failed", "error", err)
			}
		}
	}
}

// ProcessBatch pulls one batch of due pushes and attempts to deliver each,
// marking delivered rows and rescheduling failed ones with backoff. It returns
// the number of pushes successfully delivered. Exported so it can be driven
// directly in tests.
func (d *Dispatcher) ProcessBatch(ctx context.Context) (int, error) {
	events, err := d.outbox.ListPendingWorkItemRefEvents(ctx, d.cfg.BatchSize)
	if err != nil {
		return 0, fmt.Errorf("list pending pushes: %w", err)
	}
	delivered := 0
	for _, ev := range events {
		if err := d.deliver(ctx, ev.Payload); err != nil {
			next := d.now().Add(d.backoff(ev.Attempts))
			if merr := d.outbox.MarkWorkItemRefEventFailed(ctx, db.MarkWorkItemRefEventFailedParams{
				ID:            ev.ID,
				NextAttemptAt: next,
			}); merr != nil {
				return delivered, fmt.Errorf("mark push failed: %w", merr)
			}
			d.logger.Warn("work-item ref delivery failed; will retry",
				"eventId", ev.ID, "attempts", ev.Attempts+1, "nextAttemptAt", next, "error", err)
			continue
		}
		if err := d.outbox.MarkWorkItemRefEventDelivered(ctx, ev.ID); err != nil {
			return delivered, fmt.Errorf("mark push delivered: %w", err)
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

// deliver POSTs a single push payload to Tempo. A non-2xx response or any
// transport error (Tempo down) is returned as an error so the caller reschedules.
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
		return fmt.Errorf("post push: %w", err)
	}
	defer resp.Body.Close()
	// Drain so the connection can be reused.
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("tempo work-item-refs returned status %d", resp.StatusCode)
	}
	return nil
}
