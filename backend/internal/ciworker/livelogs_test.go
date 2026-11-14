package ciworker

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"
)

type fakeShipper struct {
	mu      sync.Mutex
	batches [][]LogLine
	fail    bool
}

func (f *fakeShipper) Logs(_ context.Context, _ string, _ uuid.UUID, lines []LogLine) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail {
		return errors.New("down")
	}
	f.batches = append(f.batches, lines)
	return nil
}

func TestLiveLogsShipInOrderAndFlushOnClose(t *testing.T) {
	f := &fakeShipper{}
	l := startLiveLogs(f, "w", uuid.New())
	for i := 0; i < liveMaxBatch+3; i++ {
		l.sink("build", "step", string(rune('a'+i%26)))
	}
	l.close()
	var all []LogLine
	for _, b := range f.batches {
		if len(b) > liveMaxBatch {
			t.Fatalf("batch of %d exceeds max %d", len(b), liveMaxBatch)
		}
		all = append(all, b...)
	}
	if len(all) != liveMaxBatch+3 {
		t.Fatalf("shipped %d lines, want %d", len(all), liveMaxBatch+3)
	}
	for i, ln := range all {
		if ln.Line != string(rune('a'+i%26)) {
			t.Fatalf("line %d out of order: %q", i, ln.Line)
		}
	}
}

func TestLiveLogsFailureNeverBlocks(t *testing.T) {
	f := &fakeShipper{fail: true}
	l := startLiveLogs(f, "w", uuid.New())
	for i := 0; i < liveMaxBuffered+10; i++ {
		l.sink("j", "s", "x")
	}
	l.close() // must return despite the api being down
}
