package projectsync

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/nielsuitterdijk22/quill/internal/store/db"
)

// TokenSource supplies the bearer token used to authenticate the push to
// Tempo's intake endpoint. It is an interface so the static token used today
// (QUILL_TEMPO_SYNC_TOKEN) can be swapped for the Zitadel client-credentials
// machine token from PR 8.1 without touching the dispatcher.
type TokenSource interface {
	// Token returns the bearer token to present, or an empty string to send the
	// request unauthenticated (local dev).
	Token(ctx context.Context) (string, error)
}

// StaticTokenSource returns a fixed bearer token. Empty means "no auth header".
type StaticTokenSource string

// Token implements TokenSource.
func (t StaticTokenSource) Token(context.Context) (string, error) { return string(t), nil }

// Outbox is the store surface the dispatcher needs. *store.Store satisfies it
// via its embedded *db.Queries; tests can supply an in-memory fake.
type Outbox interface {
	ListPendingProjectSyncEvents(ctx context.Context, limit int32) ([]db.ProjectSyncOutbox, error)
	MarkProjectSyncEventDelivered(ctx context.Context, id uuid.UUID) error
	MarkProjectSyncEventFailed(ctx context.Context, arg db.MarkProjectSyncEventFailedParams) error
}

// Default dispatcher tuning.
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
	// URL is Tempo's project-mirror intake endpoint. Empty disables sync entirely
	// (the dispatcher stays idle).
	URL string
	// PollInterval is how often the outbox is polled for pending events.
	PollInterval time.Duration
	// BatchSize caps how many pending events are pulled per poll.
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

// Dispatcher polls the project-sync outbox and delivers undelivered events to
// Tempo with retry and exponential backoff. It is the reliable half of the
// transactional outbox: the mutation writes a row; the dispatcher makes sure it
// reaches Tempo eventually, even across Tempo restarts.
type Dispatcher struct {
	cfg    Config
	outbox Outbox
	tokens TokenSource
	client *http.Client
	logger *slog.Logger
	// now is overridable in tests to exercise backoff scheduling deterministically.
	now func() time.Time
}

// NewDispatcher builds a Dispatcher. tokens may be nil (treated as no auth).
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

// Enabled reports whether sync is configured. When false the dispatcher is idle.
func (d *Dispatcher) Enabled() bool { return d.cfg.URL != "" }

// Run polls the outbox until ctx is cancelled. It is a no-op when sync is
// disabled (URL empty), so callers can start it unconditionally.
func (d *Dispatcher) Run(ctx context.Context) {
	if !d.Enabled() {
		d.logger.Info("project sync disabled (QUILL_TEMPO_SYNC_URL empty); dispatcher idle")
		return
	}
	d.logger.Info("project sync dispatcher started", "url", d.cfg.URL, "pollInterval", d.cfg.PollInterval)
	ticker := time.NewTicker(d.cfg.PollInterval)
	defer ticker.Stop()

	// Drain any backlog immediately on startup rather than waiting a full tick.
	if _, err := d.ProcessBatch(ctx); err != nil && ctx.Err() == nil {
		d.logger.Warn("project sync batch failed", "error", err)
	}
	for {
		select {
		case <-ctx.Done():
			d.logger.Info("project sync dispatcher stopped")
			return
		case <-ticker.C:
			if _, err := d.ProcessBatch(ctx); err != nil && ctx.Err() == nil {
				d.logger.Warn("project sync batch failed", "error", err)
			}
		}
	}
}

// ProcessBatch pulls one batch of due events and attempts to deliver each,
// marking delivered rows and rescheduling failed ones with backoff. It returns
// the number of events successfully delivered. Exported so it can be driven
// directly in tests.
func (d *Dispatcher) ProcessBatch(ctx context.Context) (int, error) {
	events, err := d.outbox.ListPendingProjectSyncEvents(ctx, d.cfg.BatchSize)
	if err != nil {
		return 0, fmt.Errorf("list pending events: %w", err)
	}
	delivered := 0
	for _, ev := range events {
		if err := d.deliver(ctx, ev.Payload); err != nil {
			next := d.now().Add(d.backoff(ev.Attempts))
			if merr := d.outbox.MarkProjectSyncEventFailed(ctx, db.MarkProjectSyncEventFailedParams{
				ID:            ev.ID,
				NextAttemptAt: next,
			}); merr != nil {
				return delivered, fmt.Errorf("mark event failed: %w", merr)
			}
			d.logger.Warn("project sync delivery failed; will retry",
				"eventId", ev.ID, "attempts", ev.Attempts+1, "nextAttemptAt", next, "error", err)
			continue
		}
		if err := d.outbox.MarkProjectSyncEventDelivered(ctx, ev.ID); err != nil {
			return delivered, fmt.Errorf("mark event delivered: %w", err)
		}
		delivered++
	}
	return delivered, nil
}

// backoff returns the delay before the next attempt given the number of
// attempts already made: base * 2^attempts, capped at MaxBackoff.
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

// deliver POSTs a single event payload to Tempo. A non-2xx response or any
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
		return fmt.Errorf("post event: %w", err)
	}
	defer resp.Body.Close()
	// Drain so the connection can be reused.
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("tempo intake returned status %d", resp.StatusCode)
	}
	return nil
}
