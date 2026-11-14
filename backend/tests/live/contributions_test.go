package live

import (
	"encoding/json"
	"net/http"
	"testing"
)

// TestContributionsAndCommitEmails: the contribution calendar is served by
// the engine index (not Forgejo), and commit emails round-trip and validate.
func TestContributionsAndCommitEmails(t *testing.T) {
	requireLive(t)
	token := login(t)

	st, body := apiCall(t, token, http.MethodGet, "/me/contributions", nil)
	var c struct {
		Contributions []struct {
			Timestamp     int64 `json:"timestamp"`
			Contributions int   `json:"contributions"`
		} `json:"contributions"`
	}
	if st != http.StatusOK || json.Unmarshal(body, &c) != nil || c.Contributions == nil {
		t.Fatalf("contributions: %d %s", st, body)
	}
	t.Logf("%d active days", len(c.Contributions))

	if st, body := apiCall(t, token, http.MethodPut, "/me/commit-emails", map[string]any{"emails": []string{"nope"}}); st != http.StatusBadRequest {
		t.Fatalf("invalid commit email: %d %s", st, body)
	}
	st, body = apiCall(t, token, http.MethodPut, "/me/commit-emails", map[string]any{"emails": []string{"E2E-Alt@Example.test"}})
	if st != http.StatusOK {
		t.Fatalf("set commit emails: %d %s", st, body)
	}
	t.Cleanup(func() { apiCall(t, token, http.MethodPut, "/me/commit-emails", map[string]any{"emails": []string{}}) })
	st, body = apiCall(t, token, http.MethodGet, "/me/commit-emails", nil)
	var e struct {
		Primary string   `json:"primary"`
		Emails  []string `json:"emails"`
	}
	if st != http.StatusOK || json.Unmarshal(body, &e) != nil || len(e.Emails) != 1 || e.Emails[0] != "e2e-alt@example.test" || e.Primary == "" {
		t.Fatalf("commit emails: %d %s", st, body)
	}
}
