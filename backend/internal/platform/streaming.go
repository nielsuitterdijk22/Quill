package platform

import (
	"sync"
)

// LogLine is a single captured output line from a running pipeline step.
type LogLine struct {
	JobKey   string
	StepName string
	Line     string
}

// LogStream provides a subscribe/replay interface for a pipeline run's live
// log output. Implementations are either backed by an in-memory broadcaster
// (active run) or a static snapshot synthesised from the database (completed run).
type LogStream interface {
	// Subscribe registers a subscriber (identified by id) and returns:
	//   - ch: a channel for incoming log lines (nil when alreadyDone)
	//   - buffered: a snapshot of all lines emitted so far
	//   - alreadyDone: true when the run has already finished; ch is nil
	// Callers must call Unsubscribe when done, unless alreadyDone is true.
	Subscribe(id string) (ch <-chan LogLine, buffered []LogLine, alreadyDone bool)
	// Unsubscribe removes the subscriber added by Subscribe.
	Unsubscribe(id string)
	// Status returns the final run status. Valid only after the channel returned
	// by Subscribe is closed or alreadyDone was true.
	Status() string
}

// logBroadcaster is the live LogStream implementation used for active runs.
// It fans log lines out to all subscribers and buffers them for late joiners.
type logBroadcaster struct {
	mu       sync.Mutex
	subs     map[string]chan LogLine
	buffered []LogLine
	done     bool
	status   string
}

func newLogBroadcaster() *logBroadcaster {
	return &logBroadcaster{subs: make(map[string]chan LogLine)}
}

func (b *logBroadcaster) publish(ll LogLine) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.buffered = append(b.buffered, ll)
	for id, ch := range b.subs {
		select {
		case ch <- ll:
		default:
			// Slow subscriber: kick it rather than silently dropping lines.
			// Closing wakes its handler, which re-subscribes and backfills the
			// missed range from the buffer — capture never blocks, and no
			// subscriber ends up with a permanent gap in its log view.
			close(ch)
			delete(b.subs, id)
		}
	}
}

// finish marks the run as complete. It is idempotent.
func (b *logBroadcaster) finish(status string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.done {
		return
	}
	b.status = status
	b.done = true
	for id, ch := range b.subs {
		close(ch)
		delete(b.subs, id)
	}
}

func (b *logBroadcaster) Subscribe(id string) (<-chan LogLine, []LogLine, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	snap := make([]LogLine, len(b.buffered))
	copy(snap, b.buffered)
	if b.done {
		return nil, snap, true
	}
	ch := make(chan LogLine, 512)
	b.subs[id] = ch
	return ch, snap, false
}

func (b *logBroadcaster) Unsubscribe(id string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.subs, id)
}

func (b *logBroadcaster) Status() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.status
}

// staticLogStream satisfies LogStream for completed runs using a pre-loaded
// snapshot of log lines from the database.
type staticLogStream struct {
	lines  []LogLine
	status string
}

func (s *staticLogStream) Subscribe(_ string) (<-chan LogLine, []LogLine, bool) {
	return nil, s.lines, true
}
func (s *staticLogStream) Unsubscribe(_ string) {}
func (s *staticLogStream) Status() string       { return s.status }
