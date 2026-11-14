package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/go-chi/chi/v5"

	"github.com/nielsuitterdijk22/quill/internal/httpx"
	"github.com/nielsuitterdijk22/quill/internal/store"
	"github.com/nielsuitterdijk22/quill/internal/store/db"
)

// Live run feed over WebSocket (2026-11-14):
//
//	GET /projects/{slug}/repos/{repo}/pipelines/runs/{number}/live?workflow=…&after=<line id>
//
// Server → client JSON messages:
//
//	{"type":"run",  "run":  <run, no jobs>}          status/timing changed
//	{"type":"logs", "lines":[{id,jobKey,stepName,line}]}
//	{"type":"done", "run":  <run detail with jobs+steps+logs>}  then close
//
// The feed polls Postgres (pipeline_live_logs + the run row), so it works on
// any api instance regardless of where the job runs. A client reconnects
// with after=<last id> to resume without gaps (e.g. after a page reload or
// the container's request timeout).

const (
	livePollInterval = time.Second
	liveBatchLimit   = 1000
	livePingInterval = 20 * time.Second
	// liveMaxLifetime caps one connection; clients reconnect transparently.
	liveMaxLifetime = 270 * time.Second // under the api container request timeout (300s)
	wsProtocol      = "quill.v1"
)

type liveLine struct {
	ID       int64  `json:"id"`
	JobKey   string `json:"jobKey"`
	StepName string `json:"stepName"`
	Line     string `json:"line"`
}

func isLiveFeed(r *http.Request) bool {
	return strings.HasSuffix(r.URL.Path, "/live") && strings.Contains(r.URL.Path, "/pipelines/runs/")
}

// wsOriginPatterns allows the configured browser origins (the app host) to
// open the feed cross-origin; coder/websocket otherwise requires same-origin.
func (s *Server) wsOriginPatterns() []string {
	var out []string
	for _, o := range s.cfg.CORSAllowedOrigins {
		if u, err := url.Parse(o); err == nil && u.Host != "" {
			out = append(out, u.Host)
		}
	}
	return out
}

func (s *Server) handleRunLive(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	workflow := strings.TrimSpace(r.URL.Query().Get("workflow"))
	if workflow == "" {
		httpx.Error(w, http.StatusBadRequest, "invalid_input", "a workflow query parameter is required")
		return
	}
	number, err := strconv.Atoi(chi.URLParam(r, "number"))
	if err != nil || number <= 0 {
		httpx.Error(w, http.StatusBadRequest, "invalid_input", "run number must be a positive integer")
		return
	}
	var after int64
	if v := r.URL.Query().Get("after"); v != "" {
		after, _ = strconv.ParseInt(v, 10, 64)
	}

	live, detail, err := s.platform.OpenRunLive(r.Context(), actor, chi.URLParam(r, "slug"), chi.URLParam(r, "repo"), workflow, number)
	if err != nil {
		s.writePlatformError(w, err, "could not load run")
		return
	}

	// Plain GET (no upgrade): one JSON snapshot — the polling fallback for
	// clients whose network refuses WebSockets, served through the app proxy.
	if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		run, lines, err := live.Poll(r.Context(), after, liveBatchLimit)
		if err != nil {
			s.writePlatformError(w, err, "could not load run")
			return
		}
		out := map[string]any{"run": newRunResponse(run), "lines": toLiveLines(lines)}
		if isTerminal(run.Status) {
			d, err := live.Detail(r.Context())
			if err != nil {
				s.writePlatformError(w, err, "could not load run")
				return
			}
			out["run"] = newRunDetailResponse(d)
			out["done"] = true
		}
		httpx.JSON(w, http.StatusOK, out)
		return
	}

	// The connection outlives the server's read/write timeouts by design.
	rc := http.NewResponseController(w)
	_ = rc.SetReadDeadline(time.Time{})
	_ = rc.SetWriteDeadline(time.Time{})

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		Subprotocols:   []string{wsProtocol},
		OriginPatterns: s.wsOriginPatterns(),
	})
	if err != nil {
		return // Accept already wrote the error response
	}
	defer conn.CloseNow()

	// Detached from the request context (the router's timeout must not apply)
	// but bounded; CloseRead handles pings/close frames from the client.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), liveMaxLifetime)
	defer cancel()
	ctx = conn.CloseRead(ctx)

	send := func(v any) bool {
		b, err := json.Marshal(v)
		if err != nil {
			return false
		}
		wctx, wcancel := context.WithTimeout(ctx, 10*time.Second)
		defer wcancel()
		return conn.Write(wctx, websocket.MessageText, b) == nil
	}
	sendDone := func() {
		d, err := live.Detail(ctx)
		if err != nil {
			conn.Close(websocket.StatusInternalError, "could not load run")
			return
		}
		out := newRunDetailResponse(d)
		send(map[string]any{"type": "done", "run": out})
		conn.Close(websocket.StatusNormalClosure, "run finished")
	}

	if isTerminal(detail.Run.Status) {
		sendDone()
		return
	}
	lastStatus := ""
	var lastStarted time.Time
	tick := time.NewTicker(livePollInterval)
	defer tick.Stop()
	ping := time.NewTicker(livePingInterval)
	defer ping.Stop()
	for {
		run, lines, err := live.Poll(ctx, after, liveBatchLimit)
		if err != nil {
			if ctx.Err() != nil {
				conn.Close(websocket.StatusGoingAway, "reconnect")
				return
			}
			s.logger.Warn("live feed poll failed", "error", err)
		} else {
			if run.Status != lastStatus || startedAt(run) != lastStarted {
				lastStatus, lastStarted = run.Status, startedAt(run)
				if !send(map[string]any{"type": "run", "run": newRunResponse(run)}) {
					return
				}
			}
			if len(lines) > 0 {
				out := toLiveLines(lines)
				after = lines[len(lines)-1].ID
				if !send(map[string]any{"type": "logs", "lines": out}) {
					return
				}
				if len(lines) == liveBatchLimit {
					continue // backlog: drain without waiting
				}
			}
			if isTerminal(run.Status) {
				sendDone()
				return
			}
		}
		select {
		case <-ctx.Done():
			conn.Close(websocket.StatusGoingAway, "reconnect")
			return
		case <-ping.C:
			pctx, pcancel := context.WithTimeout(ctx, 10*time.Second)
			err := conn.Ping(pctx)
			pcancel()
			if err != nil {
				return
			}
		case <-tick.C:
		}
	}
}

func isTerminal(status string) bool {
	switch status {
	case "success", "failure", "cancelled", "skipped":
		return true
	}
	return false
}

func startedAt(r db.PipelineRun) time.Time {
	if r.StartedAt.Valid {
		return r.StartedAt.Time
	}
	return time.Time{}
}

func toLiveLines(lines []store.LiveLogLine) []liveLine {
	out := make([]liveLine, len(lines))
	for i, l := range lines {
		out[i] = liveLine{ID: l.ID, JobKey: l.JobKey, StepName: l.StepName, Line: l.Line}
	}
	return out
}
