package pipeline

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// HTTPRunner dispatches workflow execution to a separate pipeline dispatcher.
type HTTPRunner struct {
	endpoint string
	secret   string
	client   *http.Client
}

// NewHTTPRunner constructs a runner that POSTs JobSpec payloads to dispatchURL.
func NewHTTPRunner(dispatchURL, secret string) *HTTPRunner {
	return &HTTPRunner{
		endpoint: strings.TrimRight(dispatchURL, "/") + "/api/v1/runs",
		secret:   secret,
		client:   &http.Client{Timeout: 35 * time.Minute},
	}
}

// Run sends spec to the dispatcher and returns its structured result.
func (r *HTTPRunner) Run(ctx context.Context, spec JobSpec) (RunResult, error) {
	body, err := json.Marshal(spec)
	if err != nil {
		return RunResult{}, fmt.Errorf("encode dispatch request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.endpoint, bytes.NewReader(body))
	if err != nil {
		return RunResult{}, fmt.Errorf("create dispatch request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	signDispatchRequest(req, body, r.secret)

	resp, err := r.client.Do(req)
	if err != nil {
		return RunResult{}, fmt.Errorf("dispatch workflow: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var out struct {
			Error   string `json:"error"`
			Message string `json:"message"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&out)
		if out.Message == "" {
			out.Message = resp.Status
		}
		return RunResult{}, fmt.Errorf("dispatch workflow: %s", out.Message)
	}

	var result RunResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return RunResult{}, fmt.Errorf("decode dispatch response: %w", err)
	}
	return result, nil
}

func signDispatchRequest(req *http.Request, body []byte, secret string) {
	if secret == "" {
		return
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	req.Header.Set("X-Quill-Dispatch-Signature", hex.EncodeToString(mac.Sum(nil)))
}
