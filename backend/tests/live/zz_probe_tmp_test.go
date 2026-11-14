package live

import (
	"net/http"
	"testing"
	"time"
)

func TestZZRepoProbe(t *testing.T) {
	requireLive(t)
	token := login(t)
	for _, p := range []string{"/me/projects", "/projects/e2e-tester/repos", "/projects/e2e-tester/repos/pipeline-showcase", "/me/pulls"} {
		t0 := time.Now()
		st, b := apiCall(t, token, http.MethodGet, p, nil)
		t.Logf("API %s -> %d in %s: %.200s", p, st, time.Since(t0).Round(time.Millisecond), b)
	}
	c := webSession(t)
	for _, p := range []string{"/repositories", "/e2e-tester/pipeline-showcase", "/"} {
		t0 := time.Now()
		st, _, b := get(t, c, appURL+p, nil)
		t.Logf("WEB %s -> %d in %s len=%d", p, st, time.Since(t0).Round(time.Millisecond), len(b))
	}
}
