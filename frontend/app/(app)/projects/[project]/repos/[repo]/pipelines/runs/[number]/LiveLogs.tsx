"use client";

import { useEffect, useRef, useState } from "react";
import { useRouter } from "next/navigation";
import { useQuillAuth } from "@/components/auth/context";

interface LogLine {
  jobKey: string;
  stepName: string;
  line: string;
}

interface LogGroup {
  jobKey: string;
  stepName: string;
  lines: string[];
}

function groupLines(lines: LogLine[]): LogGroup[] {
  const groups: LogGroup[] = [];
  for (const ll of lines) {
    const last = groups.at(-1);
    if (last && last.jobKey === ll.jobKey && last.stepName === ll.stepName) {
      last.lines.push(ll.line);
    } else {
      groups.push({ jobKey: ll.jobKey, stepName: ll.stepName, lines: [ll.line] });
    }
  }
  return groups;
}

export function LiveLogs({
  project,
  repo,
  runNumber,
  workflowPath,
}: {
  project: string;
  repo: string;
  runNumber: number;
  workflowPath: string;
}) {
  const [lines, setLines] = useState<LogLine[]>([]);
  const [done, setDone] = useState(false);
  const [error, setError] = useState(false);
  const [reconnecting, setReconnecting] = useState(false);
  const bottomRef = useRef<HTMLDivElement>(null);
  const router = useRouter();
  const { getToken } = useQuillAuth();

  const logsUrl =
    `/api/backend/projects/${encodeURIComponent(project)}/repos/${encodeURIComponent(repo)}` +
    `/pipelines/runs/${runNumber}/logs?workflow=${encodeURIComponent(workflowPath)}`;

  useEffect(() => {
    const controller = new AbortController();
    let cancelled = false;
    // Id of the last log event received. The server tags every log event with
    // its position in the run's log; on reconnect we send it back as
    // Last-Event-ID so the replay resumes where the previous connection broke.
    let lastId = 0;

    // Opens one SSE connection and pumps events until it ends.
    // "done" = run finished, "fatal" = auth/4xx (no point retrying),
    // "retry" = connection dropped mid-stream.
    const connectOnce = async (): Promise<"done" | "retry" | "fatal"> => {
      let token: string | null = null;
      try {
        token = await getToken();
      } catch {
        // proceed without token; middleware will reject if auth is required
      }

      const res = await fetch(logsUrl, {
        headers: {
          ...(token ? { Authorization: `Bearer ${token}` } : {}),
          ...(lastId > 0 ? { "Last-Event-ID": String(lastId) } : {}),
        },
        signal: controller.signal,
      });

      if (!res.ok || !res.body) {
        return res.status >= 400 && res.status < 500 ? "fatal" : "retry";
      }
      setReconnecting(false);

      const reader = res.body.getReader();
      const dec = new TextDecoder();
      let buf = "";
      let ename = "";
      let edata = "";
      let eid = 0;

      while (true) {
        const { done: streamDone, value } = await reader.read();
        if (streamDone) return "retry"; // stream closed without a done event

        buf += dec.decode(value, { stream: true });
        const parts = buf.split("\n");
        buf = parts.pop() ?? "";

        for (const part of parts) {
          if (part.startsWith("id:")) {
            eid = parseInt(part.slice(3).trim(), 10) || 0;
          } else if (part.startsWith("event:")) {
            ename = part.slice(6).trim();
          } else if (part.startsWith("data:")) {
            edata = part.slice(5).trim();
          } else if (part === "") {
            if (ename === "log" && edata) {
              try {
                const ll = JSON.parse(edata) as LogLine;
                setLines((prev) => [...prev, ll]);
                if (eid > 0) lastId = eid;
                bottomRef.current?.scrollIntoView({ behavior: "smooth" });
              } catch {
                // ignore malformed events
              }
            } else if (ename === "done") {
              reader.cancel().catch(() => {});
              return "done";
            }
            ename = "";
            edata = "";
            eid = 0;
          }
        }
      }
    };

    (async () => {
      // Reconnect with backoff. Attempts reset whenever a connection makes
      // progress, so only consecutive dead connections count toward giving up.
      let attempts = 0;
      while (!cancelled) {
        const before = lastId;
        let outcome: "done" | "retry" | "fatal";
        try {
          outcome = await connectOnce();
        } catch {
          if (cancelled) return;
          outcome = "retry";
        }
        if (cancelled) return;

        if (outcome === "done") {
          setDone(true);
          setTimeout(() => router.refresh(), 1000);
          return;
        }
        if (outcome === "fatal") {
          setError(true);
          return;
        }

        attempts = lastId > before ? 1 : attempts + 1;
        if (attempts > 6) {
          setError(true);
          return;
        }
        setReconnecting(true);
        await new Promise((r) => setTimeout(r, Math.min(1000 * 2 ** (attempts - 1), 10000)));
      }
    })();

    return () => {
      cancelled = true;
      controller.abort();
    };
    // logsUrl encodes project/repo/runNumber/workflowPath; stable for this run
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [logsUrl]);

  const groups = groupLines(lines);

  return (
    <div className="live-logs">
      {groups.map((g, gi) => (
        <details className="pipeline-step" key={gi} open>
          <summary>
            <span className="nm">
              {g.jobKey !== g.stepName ? `${g.jobKey} / ${g.stepName}` : g.stepName}
            </span>
          </summary>
          <pre className="logs">{g.lines.join("")}</pre>
        </details>
      ))}

      {!done && !error && (
        <div className="log-tail">
          <span className="run-glyph running">⏳</span>{" "}
          {reconnecting ? "Reconnecting to log stream…" : "Streaming logs…"}
          <div ref={bottomRef} />
        </div>
      )}

      {error && (
        <div className="banner">
          Log stream disconnected. Reload the page to try again.
        </div>
      )}
    </div>
  );
}
