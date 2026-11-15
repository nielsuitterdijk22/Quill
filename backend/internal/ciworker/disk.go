package ciworker

import (
	"context"
	"fmt"
	"strings"

	"github.com/nielsuitterdijk22/quill/internal/pipeline"
)

// Disk hygiene (2026-11-15): a CI machine keeps working through a queue, and
// every job leaves Docker images and build cache behind (a deploy builds and
// pushes four images). Quill's deploy #15 died with "no space left on device"
// on the machine that had just run deploy #14. Before each job, when free
// space is low, the worker reclaims what no container uses, keeping the act
// runner images (re-pulling those costs minutes).

// DiskReclaimer measures and frees space on the machine.
type DiskReclaimer interface {
	FreeBytes() (uint64, error)
	// Reclaim frees Docker space, keeping images whose reference starts
	// with one of keep; returns a short description of what it did.
	Reclaim(ctx context.Context, keep []string) (string, error)
}

// DiskRunner wraps a runner with the low-space cleanup.
type DiskRunner struct {
	Inner   pipeline.Runner
	Disk    DiskReclaimer
	MinFree uint64   // reclaim below this many free bytes
	Keep    []string // image reference prefixes never removed
	Log     func(msg string, args ...any)
}

// DefaultMinFree is the free-space floor before a job starts.
const DefaultMinFree = 12 << 30

func (r DiskRunner) Run(ctx context.Context, spec pipeline.JobSpec) (pipeline.RunResult, error) {
	r.ensureSpace(ctx)
	return r.Inner.Run(ctx, spec)
}

func (r DiskRunner) ensureSpace(ctx context.Context) {
	log := r.Log
	if log == nil {
		log = func(string, ...any) {}
	}
	min := r.MinFree
	if min == 0 {
		min = DefaultMinFree
	}
	free, err := r.Disk.FreeBytes()
	if err != nil {
		log("disk: cannot measure free space", "error", err)
		return
	}
	if free >= min {
		return
	}
	did, err := r.Disk.Reclaim(ctx, r.Keep)
	after, _ := r.Disk.FreeBytes()
	log("disk: reclaimed space before the job", "free_before_gib", gib(free), "free_after_gib", gib(after), "did", did, "error", err)
}

func gib(b uint64) string { return fmt.Sprintf("%.1f", float64(b)/(1<<30)) }

// keepImage reports whether any of the image's references is protected.
func keepImage(refs []string, keep []string) bool {
	for _, ref := range refs {
		for _, k := range keep {
			if k != "" && strings.HasPrefix(ref, k) {
				return true
			}
		}
	}
	return false
}
