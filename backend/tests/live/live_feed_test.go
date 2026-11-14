package live

import (
	"context"
	"encoding/json"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// TestLivePipelineFeed triggers a showcase run and follows it the way the
// run page does: a WebSocket to the api with the bearer as a subprotocol.
// It must see the run move (pending/running), receive output lines while the
// run is active, and end with the persisted job tree. Secrets stay masked in
// the live lines. Needs the showcase repo (QUILL_SHOWCASE fixture).
func TestLivePipelineFeed(t *testing.T) {
	requireLive(t)
	token := login(t)
	const proj, repo, wf = "e2e-tester", "pipeline-showcase", ".github/workflows/secrets.yml"
	st, b := apiCall(t, token, http.MethodPost, "/projects/"+proj+"/repos/"+repo+"/pipelines", map[string]any{"workflow": wf, "ref": "main"})
	if st != http.StatusCreated {
		t.Fatalf("trigger: %d %s", st, b)
	}
	var trig struct {
		Run struct {
			RunNumber int `json:"runNumber"`
		} `json:"run"`
	}
	_ = json.Unmarshal(b, &trig)

	wsURL := strings.Replace(apiURL, "https://", "wss://", 1) + "/api/v1/projects/" + proj + "/repos/" + repo +
		"/pipelines/runs/" + itoa(trig.Run.RunNumber) + "/live?workflow=" + wf
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()

	var (
		statuses   []string
		liveLines  int
		linesWhile int
		done       map[string]any
		lastID     float64
		reconnects int
	)
	for done == nil {
		u := wsURL
		if lastID > 0 {
			u += "&after=" + itoa(int(lastID))
		}
		conn, resp, err := websocket.Dial(ctx, u, &websocket.DialOptions{
			Subprotocols: []string{"quill.v1", "bearer." + token},
			HTTPHeader:   http.Header{"Origin": {appURL}},
		})
		if err != nil {
			code := 0
			if resp != nil {
				code = resp.StatusCode
			}
			t.Fatalf("dial (%d): %v", code, err)
		}
		if conn.Subprotocol() != "quill.v1" {
			t.Fatalf("server picked subprotocol %q", conn.Subprotocol())
		}
		conn.SetReadLimit(8 << 20)
		for {
			_, data, err := conn.Read(ctx)
			if err != nil {
				break // closed: done, or the 270 s lifetime → reconnect
			}
			var m map[string]any
			if err := json.Unmarshal(data, &m); err != nil {
				t.Fatalf("bad message: %v", err)
			}
			switch m["type"] {
			case "run":
				s := m["run"].(map[string]any)["status"].(string)
				statuses = append(statuses, s)
			case "logs":
				ls := m["lines"].([]any)
				liveLines += len(ls)
				if len(statuses) > 0 && statuses[len(statuses)-1] == "running" {
					linesWhile += len(ls)
				}
				for _, l := range ls {
					lm := l.(map[string]any)
					lastID = lm["id"].(float64)
					if secretRe.MatchString(lm["line"].(string)) {
						t.Fatalf("secret in live line: %q", lm["line"])
					}
				}
			case "done":
				done = m["run"].(map[string]any)
			}
		}
		conn.CloseNow()
		if done == nil {
			reconnects++
			if reconnects > 3 {
				t.Fatal("feed closed repeatedly without a done message")
			}
		}
	}
	t.Logf("statuses=%v liveLines=%d whileRunning=%d reconnects=%d final=%v", statuses, liveLines, linesWhile, reconnects, done["status"])
	if len(statuses) == 0 {
		t.Fatal("no run status messages")
	}
	if linesWhile == 0 {
		t.Error("no output lines arrived while the run was running (live logs broken)")
	}
	jobs, _ := done["jobs"].([]any)
	if len(jobs) == 0 {
		t.Errorf("done message has no job tree: %v", done)
	}
	if done["status"] != "success" {
		t.Errorf("run finished %v", done["status"])
	}

	// Reconnect after the end: immediately a done message (page reload case).
	conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		Subprotocols: []string{"quill.v1", "bearer." + token},
		HTTPHeader:   http.Header{"Origin": {appURL}},
	})
	if err != nil {
		t.Fatalf("re-dial: %v", err)
	}
	_, data, err := conn.Read(ctx)
	conn.CloseNow()
	if err != nil || !strings.Contains(string(data), `"type":"done"`) {
		t.Fatalf("reload of a finished run: %v %.200s", err, data)
	}

	// Without a bearer the handshake is refused.
	if _, resp, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{HTTPHeader: http.Header{"Origin": {appURL}}}); err == nil || resp == nil || resp.StatusCode != http.StatusUnauthorized {
		code := 0
		if resp != nil {
			code = resp.StatusCode
		}
		t.Fatalf("anonymous dial must be 401, got %d %v", code, err)
	}
}

// secretRe matches the showcase's DEMO_SECRET shape ("showcase-<hex>").
var secretRe = regexp.MustCompile(`showcase-[0-9a-f]{16,}`)

func itoa(n int) string { return strconv.Itoa(n) }
