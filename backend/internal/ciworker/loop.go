package ciworker

import (
	"context"
	"sync"
	"time"

	"github.com/nielsuitterdijk22/quill/internal/pipeline"
)

// heartbeatInterval stays well under the api's queueVisibilityTimeout (10 min)
// so a healthy long-running job is never reclaimed mid-flight.
const heartbeatInterval = 3 * time.Minute

// Terminator disposes of the worker's compute once it goes idle (the ephemeral
// CI instance deletes itself; a nil terminator is a plain process exit).
type Terminator func(ctx context.Context) error

// Drain claims and executes queued jobs until the queue stays empty longer
// than idleExit, then invokes terminate (self-termination) and returns. Every
// claimed job is heartbeat-refreshed while its workflow runs; report delivery
// is best-effort-then-logged — a lost report fails the claim's visibility
// timeout and the job is reclaimed by a future worker.
func Drain(ctx context.Context, client *Client, workerID string, runner pipeline.Runner, idleExit time.Duration, terminate Terminator, onInfo func(string, ...any)) (int, error) {
	processed := 0
	idleSince := time.Time{}
	for {
		// Claim the next job; an immediate re-claim after a finished job must
		// see fresh rows, so never short-circuit on a timer's cached time.
		job, err := client.Claim(ctx, workerID)
		if err != nil {
			onInfo("claim failed", "error", err)
			if idleSince.IsZero() {
				idleSince = time.Now()
			}
			if time.Since(idleSince) >= idleExit {
				retire(ctx, terminate, onInfo)
				return processed, nil
			}
			// Transient api errors retry at a modest fixed pace; the idle
			// window still bounds total time.
			sleepCtx(ctx, time.Second)
			continue
		}
		if job == nil {
			if idleSince.IsZero() {
				idleSince = time.Now()
			}
			if elapsed := time.Since(idleSince); elapsed >= idleExit {
				retire(ctx, terminate, onInfo)
				return processed, nil
			} else {
				// Sleep exactly out the idle window: the common exit is one
				// clean poll past it.
				sleepCtx(ctx, idleExit-elapsed)
				continue
			}
		}
		idleSince = time.Time{}
		processed++
		runClaimedJob(ctx, client, workerID, runner, job, onInfo)
	}
}

// runClaimedJob executes one claimed job with heartbeats and reports back.
func runClaimedJob(ctx context.Context, client *Client, workerID string, runner pipeline.Runner, job *ClaimedJob, onInfo func(string, ...any)) {
	// Heartbeat in the background until the run finishes.
	hbCtx, hbStop := context.WithCancel(context.WithoutCancel(ctx))
	defer hbStop()
	var lostWg sync.WaitGroup
	lost := make(chan struct{}, 1)
	lostWg.Add(1)
	go func() {
		defer lostWg.Done()
		t := time.NewTicker(heartbeatInterval)
		defer t.Stop()
		for {
			select {
			case <-hbCtx.Done():
				return
			case <-t.C:
				ok, err := client.Heartbeat(hbCtx, workerID, job.QueueID)
				if err != nil {
					onInfo("heartbeat failed", "error", err)
					continue
				}
				if !ok {
					// Reclaimed by another worker: our report would be
					// refused. Abandon the run result, keep executing only
					// so the process doesn't die mid-write; the report is
					// skipped below.
					select {
					case lost <- struct{}{}:
					default:
					}
					return
				}
			}
		}
	}()

	result, runErr := runner.Run(hbCtx, job.Spec)
	hbStop()
	lostWg.Wait()

	select {
	case <-lost:
		onInfo("claim lost mid-run; result discarded", "queue", job.QueueID)
		return
	default:
	}

	reportCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	if runErr != nil {
		onInfo("job failed to start", "queue", job.QueueID, "error", runErr.Error())
		_ = client.Report(reportCtx, workerID, job.QueueID, nil, runErr.Error())
		return
	}
	if err := client.Report(reportCtx, workerID, job.QueueID, &result, ""); err != nil {
		onInfo("report delivery failed (job will be reclaimed)", "queue", job.QueueID, "error", err)
	}
}

// retire runs the terminator; a failure is logged but does not block exit —
// the broker's TTL killer is the backstop for orphaned instances.
func retire(ctx context.Context, terminate Terminator, onInfo func(string, ...any)) {
	if terminate == nil {
		return
	}
	tctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 60*time.Second)
	defer cancel()
	if err := terminate(tctx); err != nil {
		onInfo("self-termination failed (broker TTL will clean up)", "error", err)
	}
}

// sleepCtx sleeps until d elapses or ctx is done.
func sleepCtx(ctx context.Context, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}
