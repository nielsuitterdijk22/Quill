package ciworker

import (
	"context"
	"testing"

	"github.com/nielsuitterdijk22/quill/internal/pipeline"
)

type fakeDisk struct {
	free     uint64
	reclaims int
	kept     []string
}

func (f *fakeDisk) FreeBytes() (uint64, error) { return f.free, nil }
func (f *fakeDisk) Reclaim(_ context.Context, keep []string) (string, error) {
	f.reclaims++
	f.kept = keep
	f.free += 20 << 30
	return "ok", nil
}

type countRunner struct{ runs int }

func (c *countRunner) Run(context.Context, pipeline.JobSpec) (pipeline.RunResult, error) {
	c.runs++
	return pipeline.RunResult{Status: pipeline.StatusSuccess}, nil
}

// Space is reclaimed before a job only when the disk runs low, keeping the
// runner images; the job runs either way.
func TestDiskRunner(t *testing.T) {
	d := &fakeDisk{free: 30 << 30}
	inner := &countRunner{}
	r := DiskRunner{Inner: inner, Disk: d, Keep: []string{"catthehacker/ubuntu"}}
	if _, err := r.Run(context.Background(), pipeline.JobSpec{}); err != nil || d.reclaims != 0 {
		t.Fatalf("plenty of space: reclaims=%d err=%v", d.reclaims, err)
	}
	d.free = 3 << 30
	if _, err := r.Run(context.Background(), pipeline.JobSpec{}); err != nil || d.reclaims != 1 || inner.runs != 2 {
		t.Fatalf("low space: reclaims=%d runs=%d err=%v", d.reclaims, inner.runs, err)
	}
	if len(d.kept) != 1 || d.kept[0] != "catthehacker/ubuntu" {
		t.Fatalf("keep list = %v", d.kept)
	}
}

func TestKeepImage(t *testing.T) {
	keep := []string{"catthehacker/ubuntu", "rg.fr-par.scw.cloud/quill-mvp/quill-ciworker"}
	if !keepImage([]string{"catthehacker/ubuntu:act-latest"}, keep) {
		t.Error("runner image not kept")
	}
	if keepImage([]string{"rg.fr-par.scw.cloud/quill-mvp/quill-web:abc"}, keep) {
		t.Error("a built deploy image was kept")
	}
	if keepImage(nil, keep) {
		t.Error("dangling image kept")
	}
}
