"use client";

import type { AuditLogEntry } from "../../../lib/api";

function actionBadgeClass(action: string): string {
  if (action.startsWith("auth.")) return "badge-neutral";
  if (action.startsWith("repo.")) return "badge-blue";
  if (action.startsWith("pr.")) return "badge-purple";
  if (action.startsWith("pipeline.")) return "badge-green";
  if (action.startsWith("policy.")) return "badge-orange";
  if (action.startsWith("admin.")) return "badge-red";
  return "badge-neutral";
}

function formatDate(iso: string): string {
  try {
    return new Date(iso).toLocaleString(undefined, {
      month: "short",
      day: "numeric",
      hour: "2-digit",
      minute: "2-digit",
      second: "2-digit",
    });
  } catch {
    return iso;
  }
}

function MetadataCell({ meta }: { meta: Record<string, unknown> }) {
  const entries = Object.entries(meta).filter(([, v]) => v !== "" && v !== null);
  if (entries.length === 0) return <span className="subtle">—</span>;
  return (
    <span className="audit-meta-kv">
      {entries.map(([k, v]) => (
        <span key={k} className="mono subtle">
          {k}={String(v)}
        </span>
      ))}
    </span>
  );
}

export function AuditLogTable({ entries }: { entries: AuditLogEntry[] }) {
  return (
    <div className="panel audit-table-wrap">
      <table className="audit-table">
        <thead>
          <tr>
            <th>Time</th>
            <th>Actor</th>
            <th>Event</th>
            <th>Target</th>
            <th>Details</th>
            <th>IP</th>
          </tr>
        </thead>
        <tbody>
          {entries.map((e) => (
            <tr key={e.id}>
              <td className="mono subtle nowrap">{formatDate(e.createdAt)}</td>
              <td className="mono">{e.actorUsername || <span className="subtle">system</span>}</td>
              <td>
                <span className={`badge ${actionBadgeClass(e.action)}`}>{e.action}</span>
              </td>
              <td className="mono subtle">
                {e.targetType && <span>{e.targetType}/</span>}
                {e.targetId}
              </td>
              <td>
                <MetadataCell meta={e.metadata} />
              </td>
              <td className="mono subtle">{e.ipAddress || "—"}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
