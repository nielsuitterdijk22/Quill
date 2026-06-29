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
  const bottomRef = useRef<HTMLDivElement>(null);
  const router = useRouter();
  const { getToken } = useQuillAuth();

  const logsUrl =
    `/api/backend/projects/${encodeURIComponent(project)}/repos/${encodeURIComponent(repo)}` +
    `/pipelines/runs/${runNumber}/logs?workflow=${encodeURIComponent(workflowPath)}`;

  useEffect(() => {
    const controller = new AbortController();
    let cancelled = false;

    (async () => {
      let token: string | null = null;
      try {
        token = await getToken();
      } catch {
        // proceed without token; middleware will reject if auth is required
      }

      try {
        const res = await fetch(logsUrl, {
          headers: token ? { Authorization: `Bearer ${token}` } : {},
          signal: controller.signal,
        });

        if (!res.ok || !res.body) {
          if (!cancelled) setError(true);
          return;
        }

        const reader = res.body.getReader();
        const dec = new TextDecoder();
        let buf = "";
        let ename = "";
        let edata = "";

        outer: while (true) {
          const { done: streamDone, value } = await reader.read();
          if (streamDone) {
            // Stream closed without a done event.
            if (!cancelled) setError(true);
            break;
          }
          buf += dec.decode(value, { stream: true });
          const parts = buf.split("\n");
          buf = parts.pop() ?? "";

          for (const part of parts) {
            if (part.startsWith("event:")) {
              ename = part.slice(6).trim();
            } else if (part.startsWith("data:")) {
              edata = part.slice(5).trim();
            } else if (part === "") {
              if (ename === "log" && edata) {
                try {
                  const ll = JSON.parse(edata) as LogLine;
                  setLines((prev) => [...prev, ll]);
                  bottomRef.current?.scrollIntoView({ behavior: "smooth" });
                } catch {
                  // ignore malformed events
                }
              } else if (ename === "done") {
                setDone(true);
                reader.cancel().catch(() => {});
                setTimeout(() => router.refresh(), 1000);
                break outer;
              }
              ename = "";
              edata = "";
            }
          }
        }
      } catch {
        if (!cancelled) setError(true);
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
          <span className="run-glyph running">⏳</span> Streaming logs…
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
