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
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/nielsuitterdijk22/quill/internal/egress"
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

const (
	// maxClaimAttempts is how many times a job may be claimed and lost (worker
	// died or stopped heartbeating) before its run is failed instead of retried.
	maxClaimAttempts = 3
	// queuePendingTimeout is how long a job may wait for any worker before its
	// run fails, rather than sitting "pending" forever.
	queuePendingTimeout = 90 * time.Minute
	// reapEvery throttles the reaper: it runs lazily on worker claims and run
	// listings (the api scales to zero, so there is no loop to host it).
	reapEvery = 30 * time.Second
)

var lastReap atomic.Int64 // unix nanos of the last reap pass

// ReapStuckRuns fails runs stuck in the queue forever (see store.ReapStuckPipelineRuns)
// and records why on each, so the run page and `quill run view` say what
// happened instead of showing "running" with no jobs. Cheap and throttled:
// callers invoke it freely.
func (s *Service) ReapStuckRuns(ctx context.Context) {
	now := time.Now().UnixNano()
	last := lastReap.Load()
	if now-last < int64(reapEvery) || !lastReap.CompareAndSwap(last, now) {
		return
	}
	abandoned, unclaimed, err := s.store.ReapStuckPipelineRuns(ctx, queueVisibilityTimeout, maxClaimAttempts, queuePendingTimeout)
	if err != nil {
		s.logger.Warn("reap stuck runs failed", "error", err)
		return
	}
	fail := func(ids []uuid.UUID, cause string) {
		for _, id := range ids {
			s.recordStartupFailure(ctx, id, errors.New(cause))
			if err := s.store.DeleteLiveLogs(ctx, id); err != nil {
				s.logger.Warn("could not drop live logs", "run", id, "error", err)
			}
			s.logger.Warn("failed a stuck run", "run", id, "cause", cause)
		}
	}
	fail(abandoned, fmt.Sprintf("the CI worker stopped responding %d times in a row (no heartbeat for %s); the run was abandoned. Re-run it.", maxClaimAttempts, queueVisibilityTimeout))
	fail(unclaimed, fmt.Sprintf("no CI worker picked this run up within %s; it was cancelled. Re-run it.", queuePendingTimeout))
}

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
	src, err := s.pipelineSourceFor(ctx, repo)
	if err != nil {
		return db.PipelineRun{}, err
	}
	return s.enqueuePipelineRun(ctx, repo, src, in, yaml, commitSHA, triggeredBy)
}

// enqueuePipelineRun is EnqueuePipelineRun with the git backend resolved.
func (s *Service) enqueuePipelineRun(ctx context.Context, repo db.Repository, src pipelineSource, in TriggerInput, yaml, commitSHA string, triggeredBy uuid.NullUUID) (db.PipelineRun, error) {
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
	// Deploy gate: the run's environment (explicit or the workflow's
	// `environment:`) and its policies, decided before anything is queued.
	gate, err := s.deployGate(ctx, repo, in.EnvironmentSlug, yaml, ref, commitSHA)
	if err != nil {
		return db.PipelineRun{}, err
	}
	envID := gate.EnvID
	var secrets map[string]string
	var egressPolicy *egress.Policy
	if gate.Blocked == "" { // a refused run never sees the environment's secrets
		if secrets, err = s.resolveRunSecrets(ctx, repo.ProjectID, repo.ID, envID); err != nil {
			return db.PipelineRun{}, fmt.Errorf("resolve secrets: %w", err)
		}
		// Egress policy: resolved now, like secrets (the worker enforces it).
		if egressPolicy, err = s.resolveRunEgress(ctx, repo); err != nil {
			return db.PipelineRun{}, fmt.Errorf("resolve egress policy: %w", err)
		}
	}
	cloneAuth := s.cloneAuth(src)
	fullName := src.fullName()

	pl, err := s.store.UpsertPipeline(ctx, db.UpsertPipelineParams{
		RepoID:       repo.ID,
		WorkflowPath: path,
		Name:         pipeline.WorkflowName(yaml, path),
	})
	if err != nil {
		return db.PipelineRun{}, fmt.Errorf("upsert pipeline: %w", err)
	}
	// One pull request update starts a workflow once: the same pull_request
	// event for a commit that is already waiting or running is returned instead
	// of queued again (the "opened" and "synchronize" paths, or a duplicate
	// delivery, otherwise double every PR run). Push and manual triggers always
	// run: re-pushing a commit is deliberate.
	if event == "pull_request" && commitSHA != "" {
		if dup, derr := s.store.FindInflightRun(ctx, pl.ID, event, ref, commitSHA); derr == nil {
			s.logger.Info("skipped duplicate run", "workflow", path, "event", event, "ref", ref, "existing", dup.RunNumber)
			return dup, nil
		} else if !errors.Is(derr, pgx.ErrNoRows) {
			s.logger.Warn("duplicate-run check failed; queueing anyway", "workflow", path, "error", derr)
		}
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
	if blocked, err := s.recordGate(ctx, run, gate); err != nil || blocked {
		return run, err
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
		Egress:          egressPolicy,
		PullRequest:     in.PullRequest,
	}
	// CI secret scope: an isolated CI instance only ever serves runs that
	// draw the same secrets (repository + environment).
	scope := repo.ID.String() + "/-"
	if envID.Valid {
		scope = repo.ID.String() + "/" + envID.UUID.String()
	}
	if err := s.enqueueSpec(ctx, run, tenantID, spec, scope); err != nil {
		return run, err
	}
	// A newer push supersedes this workflow's waiting runs of older commits on
	// the same ref (default since 2026-11-15: a repo's CI machine runs jobs one
	// at a time, so checking a replaced commit only delays the current one).
	// A `concurrency:` group also drops waiting runs of the same commit, as on
	// GitHub; `env: QUILL_KEEP_SUPERSEDED_RUNS: "true"` opts out.
	group := pipeline.HasConcurrency(yaml)
	if (event == "push" || event == "pull_request") && (group || !pipeline.KeepsSupersededRuns(yaml)) {
		if gone, err := s.store.SupersedePendingRuns(ctx, pl.ID, ref, run.ID, group); err != nil {
			s.logger.Warn("supersede pending runs failed", "run", run.ID, "error", err)
		} else if len(gone) > 0 {
			s.logger.Info("superseded pending runs", "workflow", path, "ref", ref, "runs", gone)
		}
	}
	return run, nil
}

// enqueueSpec serialises spec and inserts the queue row. The JobSpec dict
// carries resolved secrets: the trust domain is unchanged from the inline
// path (the same JSON already travels to the dispatcher over HMAC) — the
// queue row must never be exposed through client-facing endpoints.
func (s *Service) enqueueSpec(ctx context.Context, run db.PipelineRun, tenantID uuid.NullUUID, spec pipeline.JobSpec, scope string) error {
	raw, err := json.Marshal(spec)
	if err != nil {
		return fmt.Errorf("encode job spec: %w", err)
	}
	if err := s.store.EnqueuePipelineJobScoped(ctx, run.ID, tenantID, raw, scope); err != nil {
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
		s.wakeOnceNeed(ctx, url, secret)
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
// The body carries how many repositories wait for a CI instance (isolated
// workers serve one repo each), so the broker can boot one per repo.
func (s *Service) wakeOnceNeed(ctx context.Context, url, secret string) {
	need, err := s.store.CIReposNeedingWorker(ctx, queueVisibilityTimeout)
	if err != nil {
		s.logger.Warn("broker wake: count repos needing a worker", "error", err)
		need = 1
	}
	if need == 0 {
		return
	}
	signedPost(ctx, url+"/wake", secret, map[string]any{"need": need}, s.logger)
}

// signedPost POSTs a JSON body to the broker with the dispatch HMAC.
func signedPost(ctx context.Context, url, secret string, payload any, logger *slog.Logger) int {
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		logger.Warn("broker call: build request", "error", err)
		return 0
	}
	req.Header.Set("Content-Type", "application/json")
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	req.Header.Set("X-Quill-Dispatch-Signature", hex.EncodeToString(mac.Sum(nil)))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		logger.Info("broker call failed", "url", url, "error", err)
		return 0
	}
	resp.Body.Close()
	if resp.StatusCode >= 300 {
		logger.Info("broker call refused", "url", url, "status", resp.StatusCode)
	}
	return resp.StatusCode
}

// wakeOnce is the legacy count-less wake (kept for callers without a store).
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
	s.ReapStuckRuns(ctx)
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

// ClaimQueuedJobScoped is ClaimQueuedJob for an isolated CI instance (a
// token-authenticated worker): it only ever receives jobs of the one
// repository it is bound to (bound by its first claim), so a job that breaks
// out of its container on that instance can reach nothing beyond that repo.
func (s *Service) ClaimQueuedJobScoped(ctx context.Context, workerID string) (*QueuedJob, error) {
	s.ReapStuckRuns(ctx)
	row, _, err := s.store.ClaimPipelineJobScoped(ctx, workerID, queueVisibilityTimeout)
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
	return &QueuedJob{QueueID: row.ID, RunID: row.RunID, TenantID: row.TenantID, Attempts: row.Attempts, Spec: spec}, nil
}

// TouchCIWorker records that a bound worker is alive (heartbeats, logs).
func (s *Service) TouchCIWorker(ctx context.Context, workerID string) {
	s.store.TouchCIWorker(ctx, workerID)
}

// RetireCIWorker ends a worker's binding and asks the broker to terminate
// its instance — the worker holds no cloud credentials of its own.
func (s *Service) RetireCIWorker(ctx context.Context, workerID, instanceID string) error {
	if err := s.store.RetireCIWorker(ctx, workerID); err != nil {
		return err
	}
	url, secret := s.brokerURL, s.dispatchSecret()
	if url == "" || secret == "" || instanceID == "" {
		return nil
	}
	if code := signedPost(ctx, url+"/retire", secret, map[string]string{"instance_id": instanceID}, s.logger); code != 0 && code >= 300 {
		return fmt.Errorf("%w: broker refused to retire the instance (%d)", ErrUnavailable, code)
	}
	// Other repositories may be waiting for an instance of their own.
	s.wakeBroker()
	return nil
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

	// Mask the run's secrets out of everything the worker reports, exactly as
	// the inline path does (executeRun). The queue row's spec carries the
	// values this run was given; act also traces evaluated expressions
	// (secrets included) at debug level, so EVERY line goes through the mask.
	// Fail closed: a spec we cannot decode means we cannot mask → persist no
	// logs at all rather than risk clear-text secrets.
	var spec pipeline.JobSpec
	if err := json.Unmarshal(row.Spec, &spec); err != nil {
		s.logger.Error("queue report: undecodable spec; dropping logs to stay safe", "queue", queueID, "error", err)
		if result != nil {
			for i := range result.Jobs {
				for j := range result.Jobs[i].Steps {
					result.Jobs[i].Steps[j].Logs = "[logs withheld: secret masking unavailable]\n"
				}
			}
		}
		if runErr != "" {
			runErr = "run failed (details withheld: secret masking unavailable)"
		}
	} else {
		masker := newSecretMasker(spec.Secrets)
		if result != nil {
			masker.maskResult(result)
		}
		runErr = masker.mask(runErr)
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
