// Package ciworker is the CI execution worker: the binary that runs on the
// ephemeral CI instance (serverless epic, PR A3) and drains the pipeline
// queue. It claims jobs through the api's internal dispatch endpoints
// (PR A1), executes them with the unchanged nektos/act runner against the
// instance's Docker daemon, reports results, and retires the instance when
// the queue runs dry.
package ciworker

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/nielsuitterdijk22/quill/internal/pipeline"
)

// Client talks to the api's /api/v1/internal/dispatch surface with the same
// HMAC scheme (X-Quill-Dispatch-Signature = SHA256-HMAC of the raw body) the
// API itself uses towards the legacy dispatcher — one secret, every side.
type Client struct {
	base   string
	secret string
	token  string // per-instance worker token (preferred over secret)
	http   *http.Client
}

// NewTokenClient builds a queue client that authenticates with this
// instance's worker token (Authorization: Bearer qcw1…) — the isolated CI
// shape, where the machine never holds the dispatch secret.
func NewTokenClient(baseURL, token string) *Client {
	return &Client{base: baseURL, token: token, http: &http.Client{Timeout: 30 * time.Second}}
}

// NewClient builds a queue client against baseURL (scheme://host[:port]).
func NewClient(baseURL, secret string) *Client {
	return &Client{
		base:   baseURL,
		secret: secret,
		http:   &http.Client{Timeout: 30 * time.Second},
	}
}

// ClaimedJob mirrors the api's claim response.
type ClaimedJob struct {
	QueueID  uuid.UUID
	RunID    uuid.UUID
	Attempts int32
	Spec     pipeline.JobSpec
}

func (c *Client) do(ctx context.Context, path string, in, out any) (int, error) {
	raw, err := json.Marshal(in)
	if err != nil {
		return 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+path, bytes.NewReader(raw))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	} else {
		mac := hmac.New(sha256.New, []byte(c.secret))
		mac.Write(raw)
		req.Header.Set("X-Quill-Dispatch-Signature", hex.EncodeToString(mac.Sum(nil)))
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return resp.StatusCode, fmt.Errorf("%s: %s", resp.Status, string(msg))
	}
	if resp.StatusCode == http.StatusNoContent || out == nil {
		return resp.StatusCode, nil
	}
	return resp.StatusCode, json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(out)
}

// Claim asks for the next claimable job. (nil, nil) = queue drained.
func (c *Client) Claim(ctx context.Context, workerID string) (*ClaimedJob, error) {
	var payload struct {
		QueueID  string           `json:"queue_id"`
		RunID    string           `json:"run_id"`
		Attempts int32            `json:"attempts"`
		Spec     pipeline.JobSpec `json:"spec"`
	}
	code, err := c.do(ctx, "/api/v1/internal/dispatch/claim",
		map[string]string{"worker": workerID}, &payload)
	if err != nil {
		return nil, err
	}
	if code == http.StatusNoContent {
		return nil, nil
	}
	queueID, err := uuid.Parse(payload.QueueID)
	if err != nil {
		return nil, fmt.Errorf("claim: queue_id: %w", err)
	}
	runID, err := uuid.Parse(payload.RunID)
	if err != nil {
		return nil, fmt.Errorf("claim: run_id: %w", err)
	}
	return &ClaimedJob{
		QueueID:  queueID,
		RunID:    runID,
		Attempts: payload.Attempts,
		Spec:     payload.Spec,
	}, nil
}

// Heartbeat refreshes the claim. False means the job was reclaimed: the run
// must be abandoned immediately (the other worker owns it).
func (c *Client) Heartbeat(ctx context.Context, workerID string, queueID uuid.UUID) (bool, error) {
	code, err := c.do(ctx, "/api/v1/internal/dispatch/heartbeat", map[string]string{
		"worker": workerID, "queue_id": queueID.String(),
	}, nil)
	if err != nil {
		return false, err
	}
	return code == http.StatusNoContent, nil
}

// Report delivers the outcome: exactly one of result / runErr.
func (c *Client) Report(ctx context.Context, workerID string, queueID uuid.UUID, result *pipeline.RunResult, runErr string) error {
	body := map[string]any{
		"worker":   workerID,
		"queue_id": queueID.String(),
	}
	if runErr != "" {
		body["error"] = runErr
	} else {
		body["result"] = result
	}
	_, err := c.do(ctx, "/api/v1/internal/dispatch/report", body, nil)
	return err
}

// LogLine is one live output line shipped to the api.
type LogLine struct {
	JobKey   string `json:"jobKey"`
	StepName string `json:"stepName"`
	Line     string `json:"line"`
}

// Logs ships a batch of live output lines for a claimed job.
func (c *Client) Logs(ctx context.Context, workerID string, queueID uuid.UUID, lines []LogLine) error {
	_, err := c.do(ctx, "/api/v1/internal/dispatch/logs", map[string]any{
		"worker": workerID, "queue_id": queueID.String(), "lines": lines,
	}, nil)
	return err
}

// Retire tells the api this instance is done; the api ends the worker's
// repo binding and has the broker terminate the instance (token clients
// only — the machine holds no cloud credentials).
func (c *Client) Retire(ctx context.Context) error {
	_, err := c.do(ctx, "/api/v1/internal/dispatch/retire", map[string]string{}, nil)
	return err
}

// APIRetirer is the Terminator for token clients.
func APIRetirer(c *Client) Terminator {
	return func(ctx context.Context) error { return c.Retire(ctx) }
}
