package live

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
)

var (
	gitBaseOnce sync.Once
	gitBase     string
)

// gitCloneURL builds a credentialed HTTPS clone URL against the platform's
// advertised git base — /meta's git.publicUrl (own engine, D2) with the
// Forgejo public URL as the legacy fallback — exactly what the app's clone
// widget shows. QUILL_E2E_GIT_URL overrides.
func gitCloneURL(t *testing.T, user, token, project, repo string) string {
	t.Helper()
	gitBaseOnce.Do(func() {
		gitBase = os.Getenv("QUILL_E2E_GIT_URL")
		if gitBase != "" {
			return
		}
		resp, err := http.Get(apiURL + "/api/v1/meta")
		if err != nil {
			return
		}
		defer resp.Body.Close()
		var meta struct {
			Git struct {
				PublicURL string `json:"publicUrl"`
			} `json:"git"`
			Forgejo struct {
				PublicURL string `json:"publicUrl"`
			} `json:"forgejo"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&meta)
		gitBase = meta.Git.PublicURL
		if gitBase == "" {
			gitBase = meta.Forgejo.PublicURL
		}
	})
	if gitBase == "" {
		t.Fatal("no git base: /meta advertises neither git.publicUrl nor forgejo.publicUrl")
	}
	u, err := url.Parse(strings.TrimRight(gitBase, "/") + fmt.Sprintf("/%s/%s.git", project, repo))
	if err != nil {
		t.Fatalf("git base: %v", err)
	}
	u.User = url.UserPassword(user, token)
	return u.String()
}
