import Link from "next/link";
import { notFound } from "next/navigation";

import { listAuditLog, auditLogExportUrl } from "../../../lib/api";
import { getToken, requireSession } from "../../../lib/session";
import { AuditLogTable } from "./AuditLogTable";

const PAGE_SIZE = 100;

const ACTION_CATEGORIES = [
  { label: "All events", value: "" },
  { label: "Auth", value: "auth" },
  { label: "Org", value: "org" },
  { label: "Projects", value: "project" },
  { label: "Repositories", value: "repo" },
  { label: "Pull requests", value: "pr" },
  { label: "Pipelines", value: "pipeline" },
  { label: "Policies", value: "policy" },
  { label: "Admin", value: "admin" },
];

export default async function AuditLogPage({
  searchParams,
}: {
  searchParams: { action?: string; since?: string; until?: string; offset?: string };
}) {
  const user = await requireSession();
  if (!user.isAdmin) notFound();

  const token = await getToken();
  if (!token) notFound();

  const action = searchParams.action ?? "";
  const since = searchParams.since ?? "";
  const until = searchParams.until ?? "";
  const offset = Math.max(0, parseInt(searchParams.offset ?? "0", 10) || 0);

  const res = await listAuditLog(token, {
    action: action || undefined,
    since: since || undefined,
    until: until || undefined,
    limit: PAGE_SIZE,
    offset,
  });

  const exportUrl = auditLogExportUrl({
    action: action || undefined,
    since: since || undefined,
    until: until || undefined,
  });

  if (!res.ok) {
    return (
      <>
        <div className="crumbs">
          <span>Admin</span> <span>/</span> <span>Audit log</span>
        </div>
        <h1>Audit log</h1>
        <div className="banner">{res.message}</div>
      </>
    );
  }

  const { entries, total } = res.data;
  const prevOffset = Math.max(0, offset - PAGE_SIZE);
  const nextOffset = offset + PAGE_SIZE;

  return (
    <>
      <div className="crumbs">
        <span>Admin</span> <span>/</span> <span>Audit log</span>
      </div>
      <div className="top">
        <h1>Audit log</h1>
        <a href={exportUrl} className="btn ghost" download="audit-log.csv">
          Export CSV
        </a>
      </div>

      <div className="panel">
        <form method="GET" className="audit-filters">
          <select name="action" defaultValue={action}>
            {ACTION_CATEGORIES.map((c) => (
              <option key={c.value} value={c.value}>
                {c.label}
              </option>
            ))}
          </select>
          <span className="audit-filter-sep">from</span>
          <input
            type="date"
            name="since"
            defaultValue={since ? since.slice(0, 10) : ""}
          />
          <span className="audit-filter-sep">to</span>
          <input
            type="date"
            name="until"
            defaultValue={until ? until.slice(0, 10) : ""}
          />
          <button type="submit" className="btn small">
            Apply
          </button>
          {(action || since || until) && (
            <Link href="/admin/audit-log" className="btn ghost small">
              Clear
            </Link>
          )}
        </form>

        <div className="audit-meta subtle">
          {total === 0 ? (
            "No events found."
          ) : (
            <>
              Showing {offset + 1}–{Math.min(offset + PAGE_SIZE, total)} of {total} events
            </>
          )}
        </div>
      </div>

      {entries.length > 0 && <AuditLogTable entries={entries} />}

      {total > PAGE_SIZE && (
        <div className="pagination">
          {offset > 0 ? (
            <Link
              href={buildPageUrl({ action, since, until, offset: prevOffset })}
              className="btn ghost"
            >
              ← Previous
            </Link>
          ) : (
            <span />
          )}
          {nextOffset < total && (
            <Link
              href={buildPageUrl({ action, since, until, offset: nextOffset })}
              className="btn ghost"
            >
              Next →
            </Link>
          )}
        </div>
      )}
    </>
  );
}

function buildPageUrl(params: {
  action: string;
  since: string;
  until: string;
  offset: number;
}): string {
  const q = new URLSearchParams();
  if (params.action) q.set("action", params.action);
  if (params.since) q.set("since", params.since);
  if (params.until) q.set("until", params.until);
  if (params.offset > 0) q.set("offset", String(params.offset));
  const qs = q.toString();
  return `/admin/audit-log${qs ? `?${qs}` : ""}`;
}
