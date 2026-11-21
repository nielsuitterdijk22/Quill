package server

import "testing"

// Forgejo reports a merged pull request as "closed" with merged=true; Tempo has
// to be told "merged" to resolve the work item.
func TestWebhookPullState(t *testing.T) {
	for _, c := range []struct {
		state  string
		merged bool
		want   string
	}{{"open", false, "open"}, {"closed", false, "closed"}, {"closed", true, "merged"}} {
		if got := webhookPullState(c.state, c.merged); got != c.want {
			t.Errorf("webhookPullState(%q, %v) = %q, want %q", c.state, c.merged, got, c.want)
		}
	}
}
