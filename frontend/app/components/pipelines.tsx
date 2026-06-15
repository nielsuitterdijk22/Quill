// Presentational building blocks for pipelines (CI, PR 8): status badges, a
// relative-time helper, and the step log viewer. Server components — no client
// state.

import type { PipelineRunStatus } from "../lib/api";

const STATUS: Record<PipelineRunStatus, { label: string; cls: string }> = {
  pending: { label: "Pending", cls: "" },
  running: { label: "Running", cls: "amber" },
  success: { label: "Success", cls: "green" },
  failure: { label: "Failure", cls: "red" },
  cancelled: { label: "Cancelled", cls: "" },
  skipped: { label: "Skipped", cls: "" },
};

// RunStatusBadge renders a colored badge for a run/job/step status.
export function RunStatusBadge({ status }: { status: PipelineRunStatus }) {
  const s = STATUS[status] ?? { label: status, cls: "" };
  return <span className={`badge ${s.cls}`}>{s.label}</span>;
}

// statusGlyph maps a status to a compact leading symbol used in list rows.
export function statusGlyph(status: PipelineRunStatus): string {
  switch (status) {
    case "success":
      return "✓";
    case "failure":
      return "✕";
    case "running":
      return "▷";
    case "cancelled":
      return "⊘";
    case "skipped":
      return "–";
    default:
      return "•";
  }
}

// StepLogs renders a step's captured output in a monospace block.
export function StepLogs({ logs }: { logs: string }) {
  if (!logs.trim()) {
    return <div className="empty">No output.</div>;
  }
  return <pre className="step-logs mono">{logs.replace(/\n$/, "")}</pre>;
}
