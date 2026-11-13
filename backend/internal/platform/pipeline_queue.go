package platform

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/nielsuitterdijk22/quill/internal/pipeline"
	"github.com/nielsuitterdijk22/quill/internal/store/db"
)

// Pipeline execution queue (serverless epic, PR A1).
//
// Today the api's trigger path runs the executor inline: a goroutine in the
// api process drives the VM dispatch over SSE and persists results when it
// ends. A scale-to-zero api cannot own that lifecycle (the process leaves with
// the request), so in queue mode the trigger only persists the run + a queue
// row; a worker (the CI broker/Serverless Job in later PRs) claims rows via
// the dispatch API and reports results back.
//
// Invariants:
//   - claims are exclusive: FOR UPDATE SKIP LOCKED + status transition
//   - a claim lapses after queueVisibilityTimeout without a heartbeat; a
//     reclaimed run keeps its started_at and attempts count claim rounds
//   - every queue row carries the tenant, stamped at enqueue for metering
//   - the queue stays dumb: no outbox, no ordering guarantees beyond oldest-
//     first, final states live on pipeline_runs (queue rows are transport)

// queueVisibilityTimeout is how long a claim survives without a heartbeat
// before another worker may reclaim the job.
const queueVisibilityTimeout = 10 * time.Minute

// QueuedJob is a claim handed to a worker: the queue row identity plus the
// decoded JobSpec to execute.
type QueuedJob struct {
	QueueID  uuid.UUID
	RunID    uuid.UUID
	TenantID uuid.NullUUID
	Attempts int32
	Spec     pipeline.JobSpec
}

// WithQueueMode switches the trigger path from inline execution to
// enqueue-only. Off by default: production keeps the VM dispatch path until
// the broker/worker PRs land (rollback = leave the flag off).
func (s *Service) WithQueueMode() *Service {
	s.queueMode = true
	return s
}

// EnqueuePipelineRun persists a run as pending and enqueues its execution,
// bypassing Forgejo: the workflow YAML travels in as an argument. It is the
// queue-mode seam shared by TriggerRun/TriggerWebhook (which resolve the YAML
// through Forgejo first) and tests.
func (s *Service) EnqueuePipelineRun(ctx context.Context, repo db.Repository, in TriggerInput, yaml, commitSHA string, triggeredBy uuid.NullUUID) (db.PipelineRun, error) {
	path := in.WorkflowPath
	ref := in.Ref
	if ref == "" {
		ref = repo.DefaultBranch
	}
	event := in.Event
	if event == "" {
		event = "manual"
	}

	// Tenant stamp for metering (same fallback as the quota check: a repo
	// whose project row cannot be read proceeds unmetered but logged).
	tenantID := uuid.NullUUID{}
	if project, perr := s.store.GetProjectByID(ctx, repo.ProjectID); perr == nil {
		tenantID = uuid.NullUUID{UUID: project.TenantID, Valid: true}
	} else {
		s.logger.Warn("queue enqueue without tenant", "repo", repo.Slug, "error", perr)
	}

	// Same spec the inline path builds: environment-scoped secrets and clone
	// credentials resolved at enqueue time (they must never be re-resolved by
	// the worker, which is outside the trust boundary of the secret store).
	envID, err := s.resolveRunEnvironment(ctx, repo.ProjectID, in.EnvironmentSlug)
	if err != nil {
		return db.PipelineRun{}, err
	}
	secrets, err := s.resolveRunSecrets(ctx, repo.ProjectID, repo.ID, envID)
	if err != nil {
		return db.PipelineRun{}, fmt.Errorf("resolve secrets: %w", err)
	}
	cloneAuth := s.cloneAuthForRun(repo.ForgejoOwner.String, repo.ForgejoName.String)
	fullName := repo.ForgejoOwner.String + "/" + repo.ForgejoName.String
	if repo.ForgejoOwner.String == "" || repo.ForgejoName.String == "" {
		// Forgejo-independent rows (queue-mode tests, future own git engine)
		// fall back to the repo slug for act's github expressions.
		fullName = repo.Slug
	}

	pl, err := s.store.UpsertPipeline(ctx, db.UpsertPipelineParams{
		RepoID:       repo.ID,
		WorkflowPath: path,
		Name:         pipeline.WorkflowName(yaml, path),
	})
	if err != nil {
		return db.PipelineRun{}, fmt.Errorf("upsert pipeline: %w", err)
	}
	next, err := s.store.NextRunNumber(ctx, pl.ID)
	if err != nil {
		return db.PipelineRun{}, fmt.Errorf("next run number: %w", err)
	}
	run, err := s.store.CreatePipelineRun(ctx, db.CreatePipelineRunParams{
		PipelineID:  pl.ID,
		RunNumber:   int64(next),
		Status:      pipeline.StatusPending,
		Event:       event,
		Ref:         ref,
		CommitSha:   commitSHA,
		TriggeredBy: triggeredBy,
	})
	if err != nil {
		return db.PipelineRun{}, fmt.Errorf("create run: %w", err)
	}

	spec := pipeline.JobSpec{
		WorkflowYAML:    yaml,
		WorkflowPath:    path,
		Event:           event,
		Ref:             ref,
		CommitSHA:       commitSHA,
		CloneURL:        cloneAuth.URL,
		CloneAuthHeader: cloneAuth.AuthHeader,
		ServerURL:       cloneAuth.ServerURL,
		Token:           cloneAuth.Token,
		RepoFullName:    fullName,
		Secrets:         secrets,
	}
	return run, s.enqueueSpec(ctx, run, tenantID, spec)
}

// enqueueSpec serialises spec and inserts the queue row. The JobSpec dict
// carries resolved secrets: the trust domain is unchanged from the inline
// path (the same JSON already travels to the dispatcher over HMAC) — the
// queue row must never be exposed through client-facing endpoints.
func (s *Service) enqueueSpec(ctx context.Context, run db.PipelineRun, tenantID uuid.NullUUID, spec pipeline.JobSpec) error {
	raw, err := json.Marshal(spec)
	if err != nil {
		return fmt.Errorf("encode job spec: %w", err)
	}
	if err := s.store.EnqueuePipelineJob(ctx, run.ID, tenantID, raw); err != nil {
		return fmt.Errorf("enqueue job: %w", err)
	}
	s.wakeBroker()
	return nil
}

// wakeBroker asks the CI broker to ensure a worker exists. Fire-and-forget:
// a lost wake costs latency only — the broker's cron sweeper still drains the
// queue — so failures are logged, never propagated to the trigger request.
func (s *Service) wakeBroker() {
	url, secret := s.brokerURL, s.dispatchSecret()
	if url == "" || secret == "" {
		return
	}
	go func() {
		// Generous: a cold broker boots, then lists/creates/powers on an
		// instance before answering (it no longer aborts on hang-up either).
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		wakeOnce(ctx, url, secret, s.logger)
	}()
}

// dispatchSecret returns the configured dispatch HMAC secret (empty when the
// queue/dispatch trust group is not configured).
func (s *Service) dispatchSecret() string { return s.dispatchSecretValue }

// WithBroker wires the queue-mode wake path (serverless epic, PR A2).
func (s *Service) WithBroker(url, dispatchSecret string) *Service {
	s.brokerURL, s.dispatchSecretValue = strings.TrimRight(url, "/"), dispatchSecret
	return s
}

// wakeOnce signs and POSTs a wake-up. Same HMAC scheme as every dispatch leg.
func wakeOnce(ctx context.Context, url, secret string, logger *slog.Logger) {
	body := []byte("{}")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url+"/wake", bytes.NewReader(body))
	if err != nil {
		logger.Warn("broker wake: build request", "error", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	req.Header.Set("X-Quill-Dispatch-Signature", hex.EncodeToString(mac.Sum(nil)))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		logger.Info("broker wake failed (sweeper will drain)", "error", err)
		return
	}
	resp.Body.Close()
	if resp.StatusCode >= 300 {
		logger.Info("broker wake refused", "status", resp.StatusCode)
	}
}

// ClaimQueuedJob claims the oldest claimable queue row for workerID and flips
// its run to running. A nil job with a nil error means the queue is empty.
func (s *Service) ClaimQueuedJob(ctx context.Context, workerID string) (*QueuedJob, error) {
	row, err := s.store.ClaimPipelineJob(ctx, workerID, queueVisibilityTimeout)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("claim job: %w", err)
	}
	if err := s.store.SetPipelineRunRunning(ctx, row.RunID); err != nil {
		return nil, fmt.Errorf("mark run running: %w", err)
	}
	var spec pipeline.JobSpec
	if err := json.Unmarshal(row.Spec, &spec); err != nil {
		return nil, fmt.Errorf("decode job spec: %w", err)
	}
	return &QueuedJob{
		QueueID:  row.ID,
		RunID:    row.RunID,
		TenantID: row.TenantID,
		Attempts: row.Attempts,
		Spec:     spec,
	}, nil
}

// HeartbeatQueuedJob refreshes a claim's liveness. It returns false when the
// job was reclaimed by another worker (the caller must abandon the run: its
// result would be rejected), an error only on transport failure.
func (s *Service) HeartbeatQueuedJob(ctx context.Context, workerID string, queueID uuid.UUID) (bool, error) {
	return s.store.HeartbeatPipelineJob(ctx, queueID, workerID)
}

// ReportQueuedJob records a worker's outcome for the run behind queueID and
// finishes the queue row. Exactly one of result / runError carries the
// outcome: a result runs the normal persist path (jobs/steps + final status),
// an error records a startup failure and fails the run. The report is refused
// when the queue row is no longer claimed by workerID (stale worker after a
// reclaim) — the run's truth then belongs to whoever reclaimed it.
func (s *Service) ReportQueuedJob(ctx context.Context, workerID string, queueID uuid.UUID, result *pipeline.RunResult, runErr string) error {
	row, err := s.store.GetPipelineQueueJob(ctx, queueID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("load queue job: %w", err)
	}
	if row.ClaimedBy != workerID || row.Status != "claimed" {
		return ErrConflict
	}

	if runErr != "" {
		cause := errors.New(runErr)
		s.recordStartupFailure(ctx, row.RunID, cause)
		if _, ferr := s.finishRun(ctx, row.RunID, pipeline.StatusFailure); ferr != nil {
			s.logger.Warn("could not finish errored run", "run", row.RunID, "error", ferr)
		}
	} else if result != nil {
		if err := s.persistResult(ctx, row.RunID, *result); err != nil {
			s.logger.Warn("could not persist run result", "run", row.RunID, "error", err)
		}
		if _, ferr := s.finishRun(ctx, row.RunID, result.Status); ferr != nil {
			s.logger.Warn("could not finish run", "run", row.RunID, "error", ferr)
		}
	} else {
		return fmt.Errorf("%w: report carries neither result nor error", ErrInvalidInput)
	}

	finished, err := s.store.FinishPipelineJob(ctx, queueID, workerID)
	if err != nil {
		return fmt.Errorf("finish queue job: %w", err)
	}
	if !finished {
		// The row changed hands between the ownership check and the update.
		return ErrConflict
	}
	return nil
}
