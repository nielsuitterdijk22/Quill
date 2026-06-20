package pipeline

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// logCapture extracts structured per-job/per-step results from nektos/act, which
// otherwise only streams text to stdout. It plugs in two ways:
//
//   - as act's runner.JobLoggerFactory, so act logs through the logger we own;
//   - as a logrus hook on that logger, so every entry passes through Fire.
//
// act tags entries with fields we group on: jobID, stepID (a []string stack),
// stage (Pre/Main/Post), stepResult and jobResult (final outcomes), and
// raw_output (true on entries that carry a step's actual stdout/stderr). We keep
// the raw_output lines as logs and the *Result fields as statuses.
type logCapture struct {
	mu     sync.Mutex
	logger *logrus.Logger
	jobs   map[string]*jobCapture
	sink   LogSink // may be nil
}

type jobCapture struct {
	result   string
	steps    map[string]*stepCapture
	order    []string
	started  time.Time
	finished time.Time
}

type stepCapture struct {
	name     string
	outcome  string
	logs     bytes.Buffer
	started  time.Time
	finished time.Time
}

func newLogCapture(sink LogSink) *logCapture {
	l := logrus.New()
	l.SetOutput(io.Discard)
	// Capture every level: act emits step stdout at Info and some markers at
	// Debug; hooks only fire for levels the logger has enabled.
	l.SetLevel(logrus.TraceLevel)
	c := &logCapture{logger: l, jobs: make(map[string]*jobCapture), sink: sink}
	l.AddHook(c)
	return c
}

// WithJobLogger satisfies runner.JobLoggerFactory. act calls it once per job and
// then logs through the returned logger; our hook does the rest.
func (c *logCapture) WithJobLogger() *logrus.Logger { return c.logger }

// Levels satisfies logrus.Hook.
func (c *logCapture) Levels() []logrus.Level { return logrus.AllLevels }

// Fire satisfies logrus.Hook and routes an entry into the job/step tree.
func (c *logCapture) Fire(e *logrus.Entry) error {
	jobID, _ := e.Data["jobID"].(string)
	if jobID == "" {
		return nil
	}
	now := e.Time
	if now.IsZero() {
		now = time.Now()
	}
	now = now.UTC()

	c.mu.Lock()
	defer c.mu.Unlock()

	jc := c.jobs[jobID]
	if jc == nil {
		jc = &jobCapture{steps: make(map[string]*stepCapture), started: now}
		c.jobs[jobID] = jc
	}
	jc.finished = now
	if jr, ok := e.Data["jobResult"]; ok {
		jc.result = fmt.Sprintf("%v", jr)
	}

	stepID := lastStepID(e.Data["stepID"])
	if stepID == "" {
		return nil
	}
	sc := jc.steps[stepID]
	if sc == nil {
		sc = &stepCapture{started: now}
		jc.steps[stepID] = sc
		jc.order = append(jc.order, stepID)
	}
	sc.finished = now
	if name, ok := e.Data["step"].(string); ok && name != "" {
		sc.name = name
	}
	if sr, ok := e.Data["stepResult"]; ok {
		sc.outcome = fmt.Sprintf("%v", sr)
	}
	if raw, _ := e.Data["raw_output"].(bool); raw {
		line := e.Message
		if !strings.HasSuffix(line, "\n") {
			line += "\n"
		}
		sc.logs.WriteString(line)
		if c.sink != nil {
			key, step := jobID, sc.name
			c.sink(key, step, line)
		}
	}
	return nil
}

func (c *logCapture) job(jobID string) *jobCapture {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.jobs[jobID]
}

// lastStepID pulls the innermost step id from act's stepID field, which is a
// stack ([]string) for composite steps but may arrive as a bare string.
func lastStepID(v any) string {
	switch t := v.(type) {
	case []string:
		if len(t) > 0 {
			return t[len(t)-1]
		}
	case string:
		return t
	}
	return ""
}
