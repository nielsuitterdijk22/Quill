package forgejo

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// This file wraps Forgejo's markup rendering endpoint so Quill can render a
// repository's README (and other markdown) into sanitized HTML server-side,
// matching how Forgejo itself renders it — including repo-relative links and
// issue references. Forgejo sanitizes the HTML it returns, so the output is safe
// to embed.

// maxMarkupBytes caps the rendered HTML we read back from Forgejo.
const maxMarkupBytes = 1 << 20 // 1 MiB

// markupRequest is the payload for Forgejo's /markup endpoint.
type markupRequest struct {
	Text    string `json:"Text"`
	Mode    string `json:"Mode"`
	Context string `json:"Context"`
	Wiki    bool   `json:"Wiki"`
}

// RenderMarkup renders markdown text to sanitized HTML using Forgejo's markup
// engine. context is the "owner/name" of the repository the text belongs to, so
// relative links and references resolve correctly. The returned HTML is already
// sanitized by Forgejo.
func (c *Client) RenderMarkup(ctx context.Context, text, repoContext string) (string, error) {
	if c.base == "" || c.token == "" {
		return "", fmt.Errorf("forgejo client disabled")
	}
	payload, err := json.Marshal(markupRequest{
		Text:    text,
		Mode:    "gfm",
		Context: repoContext,
		Wiki:    false,
	})
	if err != nil {
		return "", fmt.Errorf("marshal markup: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/api/v1/markup", bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("Accept", "text/html")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "token "+c.token)

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("forgejo POST /markup: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", &APIError{Status: resp.StatusCode, Method: http.MethodPost, Path: "/markup", Message: readError(resp.Body)}
	}
	html, err := io.ReadAll(io.LimitReader(resp.Body, maxMarkupBytes))
	if err != nil {
		return "", fmt.Errorf("read markup: %w", err)
	}
	return string(html), nil
}
