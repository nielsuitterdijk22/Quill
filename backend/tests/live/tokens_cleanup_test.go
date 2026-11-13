package live

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"testing"
)

// TestRevokeE2EGitTokens (QUILL_E2E_REVOKE_TOKENS=1) revokes every git token of
// the e2e fixture account. The live suite mints one per test and never
// revokes; run this after a live round (with purge-e2e) to keep credentials
// from accumulating — and after any token appeared in test output.
func TestRevokeE2EGitTokens(t *testing.T) {
	requireLive(t)
	if os.Getenv("QUILL_E2E_REVOKE_TOKENS") != "1" {
		t.Skip("QUILL_E2E_REVOKE_TOKENS=1 not set")
	}
	token := login(t)
	st, b := apiCall(t, token, http.MethodGet, "/me/git-tokens", nil)
	if st != 200 {
		t.Fatalf("list tokens: %d %s", st, b)
	}
	var tokens []map[string]any
	if err := json.Unmarshal(b, &tokens); err != nil {
		t.Fatalf("token list: %v", err)
	}
	revoked := 0
	for _, raw := range tokens {
		id, _ := raw["id"].(string)
		if id == "" {
			continue
		}
		if st, b := apiCall(t, token, http.MethodDelete, "/me/git-tokens/"+id, nil); st != 200 && st != 204 {
			t.Errorf("revoke %s: %d %s", id, st, b)
			continue
		}
		revoked++
	}
	fmt.Printf("revoked %d of %d e2e git token(s)\n", revoked, len(tokens))
}
