"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useQuillAuth } from "@/components/auth/context";
import type {
  PipelineJob,
  PipelineRun,
  PipelineRunDetail,
  PipelineRunStatus,
} from "@/lib/api";

// Live run view (2026-11-14). One component owns the run's state from first
// paint to completion:
//   - while the run is active it follows the api's live feed over a
//     WebSocket (status changes + log lines, resumable by line id, so a page
//     reload or dropped connection replays without gaps); if WebSockets are
//     blocked it falls back to polling the same feed as JSON;
//   - when the run finishes the feed delivers the persisted job/step tree and
//     the view swaps to it in place — no reload needed.
// Every job and step starts folded; failures are highlighted and one click
// away ("Jump to failure").

type LiveLine = { id: number; jobKey: string; stepName: string; line: string };

type FeedMsg =
  | { type: "run"; run: PipelineRun }
  | { type: "logs"; lines: LiveLine[] }
  | { type: "done"; run: PipelineRunDetail };

type Conn = "connecting" | "live" | "polling" | "reconnecting" | "closed";

// A step as the view renders it, live or final.
type ViewStep = {
  key: string;
  name: string;
  status: PipelineRunStatus;
  logs: string;
  lineCount: number;
  lastLine: string;
  startedAt?: string;
  finishedAt?: string;
};
type ViewJob = {
  key: string;
  name: string;
  runsOn?: string;
  status: PipelineRunStatus;
  startedAt?: string;
  finishedAt?: string;
  steps: ViewStep[];
};

const ACTIVE: PipelineRunStatus[] = ["pending", "running"];
const isActive = (s: PipelineRunStatus) => ACTIVE.includes(s);

// eslint-disable-next-line no-control-regex
const ANSI = /\x1b\[[0-9;?]*[ -/]*[@-~]/g;
const clean = (s: string) => s.replace(ANSI, "").replace(/\r(?!\n)/g, "\n");

function lastNonEmptyLine(text: string): string {
  const lines = text.split("\n");
  for (let i = lines.length - 1; i >= 0; i--) {
    const t = lines[i].trim();
    if (t) return t;
  }
  return "";
}

function countLines(text: string): number {
  if (!text) return 0;
  const n = text.split("\n").length;
  return text.endsWith("\n") ? n - 1 : n;
}

function finalJobs(jobs: PipelineJob[]): ViewJob[] {
  return jobs.map((j) => ({
    key: j.key,
    name: j.name,
    runsOn: j.runsOn,
    status: j.status,
    startedAt: j.startedAt,
    finishedAt: j.finishedAt,
    steps: j.steps.map((s, i) => {
      const logs = clean(s.logs ?? "");
      return {
        key: `${j.key}/${i}`,
        name: s.name,
        status: s.status,
        logs,
        lineCount: countLines(logs),
        lastLine: lastNonEmptyLine(logs),
        startedAt: s.startedAt,
        finishedAt: s.finishedAt,
      };
    }),
  }));
}

// liveJobs groups streamed lines by job, then step, in order of appearance.
// While the run is active the newest step of each job is "running".
function liveJobs(lines: LiveLine[], active: boolean): ViewJob[] {
  const jobs: ViewJob[] = [];
  const byJob = new Map<string, ViewJob>();
  const buf = new Map<string, string[]>();
  for (const l of lines) {
    let job = byJob.get(l.jobKey);
    if (!job) {
      job = { key: l.jobKey, name: l.jobKey, status: "running", steps: [] };
      byJob.set(l.jobKey, job);
      jobs.push(job);
    }
    const stepKey = `${l.jobKey}/${l.stepName}`;
    let step = job.steps.find((s) => s.key === stepKey);
    if (!step) {
      step = { key: stepKey, name: l.stepName, status: "running", logs: "", lineCount: 0, lastLine: "" };
      job.steps.push(step);
      buf.set(stepKey, []);
    }
    buf.get(stepKey)!.push(clean(l.line));
  }
  for (const job of jobs) {
    job.steps.forEach((s, i) => {
      s.logs = (buf.get(s.key) ?? []).join("");
      s.lineCount = countLines(s.logs);
      s.lastLine = lastNonEmptyLine(s.logs);
      const newest = i === job.steps.length - 1;
      s.status = active && newest ? "running" : "success";
    });
    job.status = active ? "running" : "success";
  }
  return jobs;
}

// wsBase resolves the api's WebSocket origin: the build-time setting, else
// the api host next to the app host (app.<domain> → api.<domain>), else the
// local dev api.
function wsBase(): string {
  const configured = process.env.NEXT_PUBLIC_API_WS_URL;
  if (configured) return configured.replace(/\/$/, "");
  const { protocol, host } = window.location;
  const scheme = protocol === "https:" ? "wss:" : "ws:";
  if (host.startsWith("app.")) return `${scheme}//api.${host.slice(4)}`;
  return "ws://localhost:8080";
}

function fmtElapsed(ms: number): string {
  const s = Math.max(0, Math.floor(ms / 1000));
  if (s < 60) return `${s}s`;
  const m = Math.floor(s / 60);
  if (m < 60) return `${m}m ${String(s % 60).padStart(2, "0")}s`;
  return `${Math.floor(m / 60)}h ${String(m % 60).padStart(2, "0")}m`;
}

function span(start?: string, end?: string, now?: number): string | null {
  if (!start) return null;
  const a = Date.parse(start);
  const b = end ? Date.parse(end) : now;
  if (!Number.isFinite(a) || !b || !Number.isFinite(b) || b < a) return null;
  return fmtElapsed(b - a);
}

function useNow(enabled: boolean): number {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    if (!enabled) return;
    const t = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(t);
  }, [enabled]);
  return now;
}

const LABEL: Record<string, string> = {
  pending: "Queued",
  running: "Running",
  success: "Succeeded",
  failure: "Failed",
  cancelled: "Cancelled",
  skipped: "Skipped",
};

function Glyph({ status }: { status: PipelineRunStatus }) {
  if (status === "running") return <span className="run-spinner" aria-label="running" />;
  const ch =
    status === "success" ? "✓" : status === "failure" ? "✕" : status === "cancelled" ? "⊘" : status === "skipped" ? "–" : "•";
  return (
    <span className={`run-glyph ${status}`} aria-label={status}>
      {ch}
    </span>
  );
}

function StatusPill({ status }: { status: PipelineRunStatus }) {
  const cls =
    status === "success" ? "green" : status === "failure" ? "red" : status === "running" ? "amber" : "";
  return <span className={`badge ${cls}`}>{LABEL[status] ?? status}</span>;
}

// LogBlock renders numbered log lines in its own scroll box. While `follow`
// is on it sticks to the bottom unless the reader scrolled up.
function LogBlock({ text, follow }: { text: string; follow: boolean }) {
  const ref = useRef<HTMLDivElement>(null);
  const stick = useRef(true);
  const lines = useMemo(() => {
    const ls = text.split("\n");
    if (ls.length && ls[ls.length - 1] === "") ls.pop();
    return ls;
  }, [text]);
  useEffect(() => {
    const el = ref.current;
    if (el && follow && stick.current) el.scrollTop = el.scrollHeight;
  }, [lines, follow]);
  if (!lines.length) return <div className="log-empty">No output.</div>;
  return (
    <div
      className="log-block"
      ref={ref}
      onScroll={(e) => {
        const el = e.currentTarget;
        stick.current = el.scrollHeight - el.scrollTop - el.clientHeight < 24;
      }}
    >
      {lines.map((l, i) => (
        <div className="log-line" key={i}>
          <span className="log-ln">{i + 1}</span>
          <span className="log-tx">{l || " "}</span>
        </div>
      ))}
    </div>
  );
}

export function RunView({
  project,
  repo,
  workflowPath,
  initial,
  actions,
}: {
  project: string;
  repo: string;
  workflowPath: string;
  initial: PipelineRunDetail;
  actions: React.ReactNode;
}) {
  const { getToken } = useQuillAuth();
  const [run, setRun] = useState<PipelineRun>(initial);
  const [final, setFinal] = useState<PipelineJob[] | null>(isActive(initial.status) ? null : initial.jobs);
  const [lines, setLines] = useState<LiveLine[]>([]);
  const [conn, setConn] = useState<Conn>(isActive(initial.status) ? "connecting" : "closed");
  const [open, setOpen] = useState<Set<string>>(() => new Set());
  const lastId = useRef(0);
  const active = isActive(run.status) && final === null;
  const now = useNow(active);

  // A server re-render (e.g. after Re-run/Cancel triggers router.refresh)
  // hands in a newer snapshot: adopt it when it moved on.
  useEffect(() => {
    setRun((r) => (r.status !== initial.status || r.finishedAt !== initial.finishedAt ? initial : r));
    if (!isActive(initial.status)) setFinal(initial.jobs);
  }, [initial]);

  const apply = useCallback((m: FeedMsg) => {
    if (m.type === "run") setRun((r) => ({ ...r, ...m.run }));
    else if (m.type === "logs" && m.lines.length) {
      lastId.current = m.lines[m.lines.length - 1].id;
      setLines((prev) => prev.concat(m.lines));
    } else if (m.type === "done") {
      setRun(m.run);
      setFinal(m.run.jobs ?? []);
    }
  }, []);

  useEffect(() => {
    if (!isActive(initial.status)) return;
    let stopped = false;
    let ws: WebSocket | null = null;
    let timer: ReturnType<typeof setTimeout> | null = null;
    let wsFailures = 0;
    let attempt = 0;
    const path =
      `/projects/${encodeURIComponent(project)}/repos/${encodeURIComponent(repo)}` +
      `/pipelines/runs/${initial.runNumber}/live?workflow=${encodeURIComponent(workflowPath)}`;

    const later = (fn: () => void, ms: number) => {
      if (timer) clearTimeout(timer);
      timer = setTimeout(fn, ms);
    };

    // Polling fallback through the app's authenticated proxy.
    const poll = async () => {
      if (stopped) return;
      setConn("polling");
      try {
        const res = await fetch(`/api/backend${path}&after=${lastId.current}`, { cache: "no-store" });
        if (res.ok) {
          const body = (await res.json()) as { run: PipelineRunDetail; lines: LiveLine[]; done?: boolean };
          if (body.lines?.length) apply({ type: "logs", lines: body.lines });
          if (body.done) {
            apply({ type: "done", run: body.run });
            setConn("closed");
            return;
          }
          apply({ type: "run", run: body.run });
        }
      } catch {
        /* keep polling */
      }
      later(poll, 2000);
    };

    const connect = async () => {
      if (stopped) return;
      let token: string | null = null;
      try {
        token = await getToken();
      } catch {
        token = null;
      }
      if (stopped) return;
      if (!token) {
        void poll();
        return;
      }
      let gotMessage = false;
      try {
        ws = new WebSocket(`${wsBase()}/api/v1${path}&after=${lastId.current}`, ["quill.v1", `bearer.${token}`]);
      } catch {
        void poll();
        return;
      }
      ws.onopen = () => {
        attempt = 0;
        setConn("live");
      };
      ws.onmessage = (ev) => {
        gotMessage = true;
        wsFailures = 0;
        try {
          const m = JSON.parse(String(ev.data)) as FeedMsg;
          apply(m);
          if (m.type === "done") stopped = true;
        } catch {
          /* ignore malformed */
        }
      };
      ws.onclose = () => {
        ws = null;
        if (stopped) {
          setConn("closed");
          return;
        }
        if (!gotMessage) wsFailures++;
        // Three handshakes in a row without a single message: the network
        // (proxy, firewall) is eating WebSockets — switch to polling.
        if (wsFailures >= 3) {
          void poll();
          return;
        }
        attempt++;
        setConn("reconnecting");
        later(() => void connect(), Math.min(500 * 2 ** attempt, 8000));
      };
    };

    // Coming back to a backgrounded tab: reconnect right away.
    const onVisible = () => {
      if (document.visibilityState === "visible" && !ws && !stopped && wsFailures < 3) {
        later(() => void connect(), 0);
      }
    };
    document.addEventListener("visibilitychange", onVisible);
    void connect();
    return () => {
      stopped = true;
      document.removeEventListener("visibilitychange", onVisible);
      if (timer) clearTimeout(timer);
      ws?.close();
    };
    // initial.runNumber/workflowPath identify the run; a status change of the
    // same run must not tear the feed down.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [project, repo, workflowPath, initial.runNumber]);

  const jobs = useMemo(
    () => (final ? finalJobs(final) : liveJobs(lines, isActive(run.status))),
    [final, lines, run.status],
  );

  const toggle = (key: string) =>
    setOpen((prev) => {
      const next = new Set(prev);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      return next;
    });
  const allKeys = useMemo(() => jobs.flatMap((j) => [j.key, ...j.steps.map((s) => s.key)]), [jobs]);
  const anyOpen = open.size > 0;
  const firstFailure = useMemo(() => {
    for (const j of jobs) {
      const s = j.steps.find((st) => st.status === "failure");
      if (s) return { job: j.key, step: s.key };
      if (j.status === "failure") return { job: j.key, step: j.steps[j.steps.length - 1]?.key };
    }
    return null;
  }, [jobs]);

  const jumpToFailure = () => {
    if (!firstFailure) return;
    setOpen((prev) => {
      const next = new Set(prev);
      next.add(firstFailure.job);
      if (firstFailure.step) next.add(firstFailure.step);
      return next;
    });
    setTimeout(() => {
      document.getElementById(`step-${firstFailure.step ?? firstFailure.job}`)?.scrollIntoView({ block: "start", behavior: "smooth" });
    }, 50);
  };

  const rawLog = () => {
    const text = jobs
      .map((j) => j.steps.map((s) => `##[${j.name} / ${s.name}]\n${s.logs}`).join("\n"))
      .join("\n");
    const url = URL.createObjectURL(new Blob([text], { type: "text/plain" }));
    const a = document.createElement("a");
    a.href = url;
    a.download = `${(workflowPath.split("/").pop() ?? "run").replace(/\.ya?ml$/, "")}-${run.runNumber}.log`;
    a.click();
    URL.revokeObjectURL(url);
  };

  const workflowName = workflowPath.split("/").pop() ?? "Workflow";
  const duration = span(run.startedAt, run.finishedAt, now);
  const queuedFor = run.status === "pending" ? span(run.createdAt, undefined, now) : null;

  return (
    <>
      <div className={`panel run-hero run-${run.status}`}>
        <div className="run-hero-main">
          <span className="run-hero-glyph">
            <Glyph status={run.status} />
          </span>
          <div>
            <h1>
              {workflowName} <span className="muted">#{run.runNumber}</span>
            </h1>
            <div className="run-meta">
              <span className="mono">{workflowPath}</span>
              <span>{run.event}</span>
              <span>{run.ref || "—"}</span>
              {run.commitSha && <span className="mono">{run.commitSha.slice(0, 7)}</span>}
              <span title={new Date(run.createdAt).toLocaleString()}>
                Triggered {new Date(run.createdAt).toLocaleString()}
              </span>
              {duration && <span>{run.finishedAt ? "Took" : "Running for"} {duration}</span>}
            </div>
          </div>
        </div>
        <div className="run-hero-actions">
          <StatusPill status={run.status} />
          {actions}
        </div>
      </div>

      {run.status === "pending" && (
        <div className="panel run-waiting">
          <span className="run-spinner" aria-hidden />
          <div>
            <strong>Waiting for a runner</strong>
            <div className="sub">
              CI machines start on demand, so the first job of a burst usually waits about a
              minute. Queued for {queuedFor ?? "0s"}.
            </div>
          </div>
        </div>
      )}

      <div className="panel run-jobs">
        <div className="run-jobs-bar">
          <h2>
            Jobs <span className="tag">{jobs.length}</span>
          </h2>
          <span className="spacer" />
          {active && (
            <span className={`run-conn run-conn-${conn}`}>
              <span className="run-conn-dot" aria-hidden />
              {conn === "live"
                ? "Live"
                : conn === "polling"
                  ? "Live (polling)"
                  : conn === "reconnecting"
                    ? "Reconnecting…"
                    : "Connecting…"}
            </span>
          )}
          {firstFailure && !active && (
            <button type="button" className="btn ghost" onClick={jumpToFailure}>
              Jump to failure
            </button>
          )}
          {jobs.length > 0 && (
            <button
              type="button"
              className="btn ghost"
              onClick={() => setOpen(anyOpen ? new Set() : new Set(allKeys))}
            >
              {anyOpen ? "Collapse all" : "Expand all"}
            </button>
          )}
          {!active && jobs.length > 0 && (
            <button type="button" className="btn ghost" onClick={rawLog}>
              Download log
            </button>
          )}
        </div>

        {jobs.length === 0 ? (
          <div className="empty">
            {active
              ? run.status === "pending"
                ? "Jobs appear here as soon as a runner picks the run up."
                : "Starting… output appears here as the first step runs."
              : "This run produced no jobs."}
          </div>
        ) : (
          jobs.map((job) => {
            const jobOpen = open.has(job.key);
            const jobDur = span(job.startedAt, job.finishedAt, now);
            const failedSteps = job.steps.filter((s) => s.status === "failure").length;
            return (
              <div className={`run-job run-job-${job.status}${jobOpen ? " open" : ""}`} key={job.key} id={`step-${job.key}`}>
                <button type="button" className="run-job-head" onClick={() => toggle(job.key)} aria-expanded={jobOpen}>
                  <Glyph status={job.status} />
                  <span className="run-job-name">{job.name}</span>
                  {job.runsOn && <span className="sub mono">{job.runsOn}</span>}
                  <span className="spacer" />
                  <span className="sub">
                    {job.steps.length} step{job.steps.length === 1 ? "" : "s"}
                    {failedSteps > 0 && <span className="run-fail-count"> · {failedSteps} failed</span>}
                  </span>
                  {jobDur && <span className="sub run-dur">{jobDur}</span>}
                  <span className="run-chev" aria-hidden />
                </button>
                {jobOpen && (
                  <div className="run-steps">
                    {job.steps.map((s) => {
                      const stepOpen = open.has(s.key);
                      const dur = span(s.startedAt, s.finishedAt, now);
                      return (
                        <div className={`run-step run-step-${s.status}${stepOpen ? " open" : ""}`} key={s.key} id={`step-${s.key}`}>
                          <button type="button" className="run-step-head" onClick={() => toggle(s.key)} aria-expanded={stepOpen}>
                            <Glyph status={s.status} />
                            <span className="run-step-name">{s.name}</span>
                            {!stepOpen && s.lastLine && (s.status === "running" || s.status === "failure") && (
                              <span className="run-step-peek mono">{s.lastLine}</span>
                            )}
                            <span className="spacer" />
                            <span className="sub">{s.lineCount} line{s.lineCount === 1 ? "" : "s"}</span>
                            {dur && <span className="sub run-dur">{dur}</span>}
                            <span className="run-chev" aria-hidden />
                          </button>
                          {stepOpen && <LogBlock text={s.logs} follow={s.status === "running"} />}
                        </div>
                      );
                    })}
                  </div>
                )}
              </div>
            );
          })
        )}
      </div>
    </>
  );
}
