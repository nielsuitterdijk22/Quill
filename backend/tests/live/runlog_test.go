package live

import (
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strings"
	"testing"
)

// TestDumpRunLog (QUILL_RUNLOG=<project>/<repo>:<workflow path>:<run number>)
// prints a run's log lines minus act's debug noise — the operator's view of
// why a live run failed.
func TestDumpRunLog(t *testing.T) {
	requireLive(t)
	spec := os.Getenv("QUILL_RUNLOG")
	if spec == "" {
		t.Skip("QUILL_RUNLOG not set")
	}
	parts := strings.SplitN(spec, ":", 3)
	if len(parts) != 3 {
		t.Fatalf("QUILL_RUNLOG=%q, want project/repo:workflow:number", spec)
	}
	token := login(t)
	st, b := apiCall(t, token, http.MethodGet, fmt.Sprintf("/projects/%s/pipelines/runs/%s/logs?workflow=%s",
		strings.Replace(parts[0], "/", "/repos/", 1), parts[2], parts[1]), nil)
	if st != 200 {
		t.Fatalf("logs: %d %s", st, b)
	}
	noise := regexp.MustCompile(`evaluat|rewritten to|Writing entry|docker volume|Extracting content|docker exec cmd|Exec command|Working directory|Removed container|Cleaning up|Found revision`)
	line := regexp.MustCompile(`"jobKey":"([^"]*)","line":"((?:[^"\\]|\\.)*)","stepName":"([^"]*)"`)
	for _, m := range line.FindAllStringSubmatch(string(b), -1) {
		text := strings.ReplaceAll(strings.ReplaceAll(m[2], `\n`, ""), `\"`, `"`)
		if noise.MatchString(text) || strings.TrimSpace(text) == "" {
			continue
		}
		fmt.Printf("[%s|%s] %s\n", m[1], m[3], text)
	}
}
