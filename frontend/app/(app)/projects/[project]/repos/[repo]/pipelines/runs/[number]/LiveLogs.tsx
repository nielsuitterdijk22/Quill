"use client";

import { useEffect, useRef, useState } from "react";
import { useRouter } from "next/navigation";

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

  const logsUrl =
    `/api/backend/projects/${encodeURIComponent(project)}/repos/${encodeURIComponent(repo)}` +
    `/pipelines/runs/${runNumber}/logs?workflow=${encodeURIComponent(workflowPath)}`;

  useEffect(() => {
    const es = new EventSource(logsUrl);

    es.addEventListener("log", (e: MessageEvent) => {
      try {
        const ll = JSON.parse(e.data) as LogLine;
        setLines((prev) => [...prev, ll]);
        bottomRef.current?.scrollIntoView({ behavior: "smooth" });
      } catch {
        // ignore malformed events
      }
    });

    es.addEventListener("done", () => {
      setDone(true);
      es.close();
      // Give the DB a moment to commit before refreshing so the status badge
      // reflects the final result.
      setTimeout(() => router.refresh(), 1000);
    });

    es.onerror = () => {
      setError(true);
      es.close();
    };

    return () => {
      es.close();
    };
  }, [logsUrl, router]);

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
