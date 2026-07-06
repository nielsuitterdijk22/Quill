package pipeline

import (
	"bufio"
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// HTTPRunner dispatches workflow execution to a separate pipeline dispatcher.
// It implements the two-phase protocol:
//  1. POST /api/v1/runs → 202 { "jobID": "..." }
//  2. GET  /api/v1/runs/{jobID}/stream → SSE until "done" event
//
// Log events received over SSE are forwarded to spec.LogSink so the platform
// can stream them to SSE clients in real time. The final RunResult is extracted
// from the "done" event payload.
type HTTPRunner struct {
	endpoint     string
	streamBase   string
	secret       string
	postClient   *http.Client
	streamClient *http.Client
}

// NewHTTPRunner constructs a runner that POSTs JobSpec payloads to dispatchURL.
func NewHTTPRunner(dispatchURL, secret string) *HTTPRunner {
	base := strings.TrimRight(dispatchURL, "/")
	return &HTTPRunner{
		endpoint:     base + "/api/v1/runs",
		streamBase:   base + "/api/v1/runs",
		secret:       secret,
		postClient:   &http.Client{Timeout: 30 * time.Second},
		streamClient: &http.Client{Timeout: 40 * time.Minute},
	}
}

// Run sends spec to the dispatcher and returns its structured result.
// It blocks until the run completes, forwarding live log events to spec.LogSink.
func (r *HTTPRunner) Run(ctx context.Context, spec JobSpec) (RunResult, error) {
	// Phase 1: Submit the job and get back a job ID.
	jobID, err := r.submitJob(ctx, spec)
	if err != nil {
		return RunResult{}, err
	}

	// Phase 2: Stream SSE until the done event, relaying log lines.
	return r.streamUntilDone(ctx, jobID, spec.LogSink)
}

// StreamLogs implements LogStreamer. It opens a raw SSE connection to the
// dispatcher for the given jobID and returns the response body. The caller
// is responsible for closing the returned ReadCloser.
func (r *HTTPRunner) StreamLogs(ctx context.Context, jobID string) (io.ReadCloser, error) {
	url := r.streamBase + "/" + jobID + "/stream"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create stream request: %w", err)
	}
	resp, err := r.streamClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("connect to log stream: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("stream returned %s", resp.Status)
	}
	return resp.Body, nil
}

// submitJob sends the spec JSON to POST /api/v1/runs and returns the job ID.
func (r *HTTPRunner) submitJob(ctx context.Context, spec JobSpec) (string, error) {
	body, err := json.Marshal(spec)
	if err != nil {
		return "", fmt.Errorf("encode dispatch request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.endpoint, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("create dispatch request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	signDispatchRequest(req, body, r.secret)

	resp, err := r.postClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("dispatch workflow: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		var out struct {
			Message string `json:"message"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&out)
		if out.Message == "" {
			out.Message = resp.Status
		}
		return "", fmt.Errorf("dispatch workflow: %s", out.Message)
	}

	var accepted struct {
		JobID string `json:"jobID"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&accepted); err != nil {
		return "", fmt.Errorf("decode dispatch response: %w", err)
	}
	if accepted.JobID == "" {
		return "", fmt.Errorf("dispatch response missing jobID")
	}
	return accepted.JobID, nil
}

// streamUntilDone reads the SSE stream for jobID, forwarding log events to sink
// and returning the RunResult extracted from the "done" event. A dropped
// connection is retried with backoff: the dispatcher buffers every event (and
// keeps finished jobs around for several minutes), so a reconnect replays the
// full history — already-forwarded log events are skipped by count so the sink
// never sees duplicates.
func (r *HTTPRunner) streamUntilDone(ctx context.Context, jobID string, sink LogSink) (RunResult, error) {
	const maxAttempts = 5
	seenLogs := 0
	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return RunResult{}, ctx.Err()
			case <-time.After(time.Duration(1<<(attempt-1)) * time.Second):
			}
		}
		result, outcome, err := r.streamOnce(ctx, jobID, sink, &seenLogs)
		switch outcome {
		case streamDone:
			return result, err
		case streamFatal:
			return RunResult{}, err
		}
		lastErr = err
	}
	return RunResult{}, fmt.Errorf("log stream ended without done event after %d attempts: %w", maxAttempts, lastErr)
}

// streamOnce outcomes.
type streamOutcome int

const (
	streamDone  streamOutcome = iota // done event received; result/err are final
	streamRetry                      // connection dropped mid-stream; reconnect
	streamFatal                      // unrecoverable (e.g. job unknown); do not retry
)

// streamOnce opens one SSE connection and reads until the done event or the
// stream drops. seenLogs counts log events forwarded to the sink across
// attempts; replayed events below that count are skipped.
func (r *HTTPRunner) streamOnce(ctx context.Context, jobID string, sink LogSink, seenLogs *int) (RunResult, streamOutcome, error) {
	url := r.streamBase + "/" + jobID + "/stream"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return RunResult{}, streamFatal, fmt.Errorf("create stream request: %w", err)
	}
	resp, err := r.streamClient.Do(req)
	if err != nil {
		return RunResult{}, streamRetry, fmt.Errorf("connect to log stream: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// A non-200 means the dispatcher doesn't know this job (expired or the
		// process restarted and lost it) — reconnecting cannot recover that.
		return RunResult{}, streamFatal, fmt.Errorf("log stream returned %s", resp.Status)
	}

	// Parse SSE line by line. The default 64KiB token limit is too small for
	// verbose build output — one long line would error the whole stream — so
	// allow lines up to 1MiB (the dispatcher caps stored logs anyway).
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64<<10), 1<<20)
	logEvents := 0
	var eventName, dataLine string

	for scanner.Scan() {
		line := scanner.Text()

		switch {
		case strings.HasPrefix(line, "event:"):
			eventName = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			dataLine = strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		case line == "":
			// Blank line: dispatch the accumulated event.
			if eventName != "" && dataLine != "" {
				forward := sink
				if eventName == "log" {
					logEvents++
					if logEvents <= *seenLogs {
						forward = nil // replayed event from a previous attempt
					} else {
						*seenLogs = logEvents
					}
				}
				result, done, err := r.handleSSEEvent(eventName, dataLine, forward)
				if done {
					return result, streamDone, err
				}
			}
			eventName = ""
			dataLine = ""
		}
	}

	if err := scanner.Err(); err != nil {
		return RunResult{}, streamRetry, fmt.Errorf("read log stream: %w", err)
	}
	return RunResult{}, streamRetry, fmt.Errorf("log stream ended without done event")
}

// handleSSEEvent processes one dispatched SSE event. It returns (result, true, err)
// when the stream is finished.
func (r *HTTPRunner) handleSSEEvent(eventName, data string, sink LogSink) (RunResult, bool, error) {
	switch eventName {
	case "log":
		if sink != nil {
			var ll struct {
				JobKey   string `json:"jobKey"`
				StepName string `json:"stepName"`
				Line     string `json:"line"`
			}
			if err := json.Unmarshal([]byte(data), &ll); err == nil {
				sink(ll.JobKey, ll.StepName, ll.Line)
			}
		}
	case "done":
		var done struct {
			Status string     `json:"status"`
			Error  string     `json:"error,omitempty"`
			Result *RunResult `json:"result,omitempty"`
		}
		if err := json.Unmarshal([]byte(data), &done); err != nil {
			return RunResult{}, true, fmt.Errorf("decode done event: %w", err)
		}
		if done.Error != "" {
			return RunResult{}, true, fmt.Errorf("dispatch workflow: %s", done.Error)
		}
		if done.Result != nil {
			return *done.Result, true, nil
		}
		return RunResult{Status: done.Status}, true, nil
	}
	return RunResult{}, false, nil
}

func signDispatchRequest(req *http.Request, body []byte, secret string) {
	if secret == "" {
		return
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	req.Header.Set("X-Quill-Dispatch-Signature", hex.EncodeToString(mac.Sum(nil)))
}
