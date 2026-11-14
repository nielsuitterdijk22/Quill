package ciworker

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Live log shipping (2026-11-14): the runner's LogSink feeds a buffer that is
// flushed to the api every flushInterval (or when it reaches maxBatch lines),
// so viewers see output while the job runs instead of only at the report.
// Shipping is best-effort: a failed batch is dropped (the final report still
// carries every line in the step logs), never blocking the job.

const (
	liveFlushInterval = time.Second
	liveMaxBatch      = 500
	// liveMaxBuffered caps memory when the api is unreachable.
	liveMaxBuffered = 20000
)

type logShipper interface {
	Logs(ctx context.Context, workerID string, queueID uuid.UUID, lines []LogLine) error
}

type liveLogs struct {
	client   logShipper
	workerID string
	queueID  uuid.UUID

	mu      sync.Mutex
	buf     []LogLine
	dropped bool
	kick    chan struct{}
	stop    chan struct{}
	done    chan struct{}
}

func startLiveLogs(client logShipper, workerID string, queueID uuid.UUID) *liveLogs {
	l := &liveLogs{
		client: client, workerID: workerID, queueID: queueID,
		kick: make(chan struct{}, 1), stop: make(chan struct{}), done: make(chan struct{}),
	}
	go l.loop()
	return l
}

// sink matches pipeline.LogSink.
func (l *liveLogs) sink(jobKey, stepName, line string) {
	l.mu.Lock()
	if len(l.buf) < liveMaxBuffered {
		l.buf = append(l.buf, LogLine{JobKey: jobKey, StepName: stepName, Line: line})
	} else {
		l.dropped = true
	}
	full := len(l.buf) >= liveMaxBatch
	l.mu.Unlock()
	if full {
		select {
		case l.kick <- struct{}{}:
		default:
		}
	}
}

func (l *liveLogs) flush() {
	for {
		l.mu.Lock()
		n := len(l.buf)
		if n == 0 {
			l.mu.Unlock()
			return
		}
		if n > liveMaxBatch {
			n = liveMaxBatch
		}
		batch := append([]LogLine(nil), l.buf[:n]...)
		l.buf = l.buf[n:]
		l.mu.Unlock()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		err := l.client.Logs(ctx, l.workerID, l.queueID, batch)
		cancel()
		if err != nil {
			return // best-effort; the report carries the full logs
		}
	}
}

func (l *liveLogs) loop() {
	defer close(l.done)
	t := time.NewTicker(liveFlushInterval)
	defer t.Stop()
	for {
		select {
		case <-l.stop:
			l.flush()
			return
		case <-t.C:
			l.flush()
		case <-l.kick:
			l.flush()
		}
	}
}

// close ships the remainder; call before reporting so live lines land first.
func (l *liveLogs) close() {
	close(l.stop)
	<-l.done
}
