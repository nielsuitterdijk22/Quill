package gitengine

import (
	"bytes"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// Concurrent hydrations of one repo (a push's post-receive CI trigger racing
// the next API request, D2 cutover) write the same packed-refs / pack files.
// A shared fixed temp name let one writer rename the other's staging file
// away ("no such file" → intermittent 500s on PR create). Every concurrent
// writeAtomic must succeed and leave exactly one complete file.
func TestWriteAtomicConcurrentSameDest(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "packed-refs")
	payload := bytes.Repeat([]byte("ref line\n"), 4096)

	var wg sync.WaitGroup
	errs := make(chan error, 64)
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := writeAtomic(dest, payload); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent writeAtomic: %v", err)
	}
	got, err := os.ReadFile(dest)
	if err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("dest content wrong (err=%v, %d bytes)", err, len(got))
	}
	left, _ := filepath.Glob(filepath.Join(dir, "*.part*"))
	if len(left) != 0 {
		t.Fatalf("staging files left behind: %v", left)
	}
}
