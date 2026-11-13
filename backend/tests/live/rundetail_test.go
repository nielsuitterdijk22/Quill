package live

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
)

// TestDumpRunDetail (QUILL_RUNDETAIL=<project>/<repo>:<workflow>:<number>)
// prints a run's job/step tree with statuses.
func TestDumpRunDetail(t *testing.T) {
	requireLive(t)
	spec := os.Getenv("QUILL_RUNDETAIL")
	if spec == "" {
		t.Skip("QUILL_RUNDETAIL not set")
	}
	parts := strings.SplitN(spec, ":", 3)
	token := login(t)
	st, b := apiCall(t, token, http.MethodGet, fmt.Sprintf("/projects/%s/pipelines/runs/%s?workflow=%s",
		strings.Replace(parts[0], "/", "/repos/", 1), parts[2], parts[1]), nil)
	if st != 200 {
		t.Fatalf("run: %d %s", st, b)
	}
	var v any
	_ = json.Unmarshal(b, &v)
	out, _ := json.MarshalIndent(v, "", " ")
	for _, l := range strings.Split(string(out), "\n") {
		if strings.Contains(l, `"logs"`) {
			continue
		}
		fmt.Println(l)
	}
}
