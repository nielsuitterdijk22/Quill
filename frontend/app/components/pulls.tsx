// Presentational building blocks for pull requests (PR 6): state badges, the
// diff stat, and the unified diff view. Server components — no client state.

import type { DiffFile, PullRequest } from "../lib/api";

// pullState collapses a PR's open/closed/merged status into one label + class.
function pullState(pull: Pick<PullRequest, "state" | "merged">): {
  label: string;
  cls: string;
} {
  if (pull.merged) return { label: "Merged", cls: "accent" };
  if (pull.state === "closed") return { label: "Closed", cls: "red" };
  return { label: "Open", cls: "green" };
}

// PullStateBadge renders a colored badge for a PR's status.
export function PullStateBadge({
  pull,
}: {
  pull: Pick<PullRequest, "state" | "merged">;
}) {
  const { label, cls } = pullState(pull);
  return <span className={`badge ${cls}`}>{label}</span>;
}

// DiffStat renders the +additions / -deletions counters.
export function DiffStat({
  additions,
  deletions,
}: {
  additions: number;
  deletions: number;
}) {
  return (
    <span className="diffstat">
      <span className="add">+{additions}</span>
      <span className="del">−{deletions}</span>
    </span>
  );
}

const REVIEW_STATE: Record<string, { label: string; cls: string }> = {
  APPROVED: { label: "Approved", cls: "green" },
  REQUEST_CHANGES: { label: "Changes requested", cls: "red" },
  COMMENT: { label: "Commented", cls: "" },
  PENDING: { label: "Pending", cls: "" },
};

// ReviewStateBadge renders a colored badge for a review's verdict.
export function ReviewStateBadge({ state }: { state: string }) {
  const r = REVIEW_STATE[state] ?? { label: state, cls: "" };
  return <span className={`badge ${r.cls}`}>{r.label}</span>;
}

const STATUS_GLYPH: Record<string, string> = {
  added: "A",
  deleted: "D",
  renamed: "R",
  modified: "M",
};

// DiffView renders a parsed unified diff: one panel per file with a header and a
// gutter-numbered, color-coded hunk table.
export function DiffView({ files }: { files: DiffFile[] }) {
  if (files.length === 0) {
    return <div className="empty">No changes to display.</div>;
  }
  return (
    <div className="diff">
      {files.map((f) => (
        <div className="diff-file" key={f.path}>
          <div className="diff-file-head">
            <span className={`diff-status ${f.status}`}>
              {STATUS_GLYPH[f.status] ?? "M"}
            </span>
            <span className="mono path">
              {f.status === "renamed" ? `${f.oldPath} → ${f.path}` : f.path}
            </span>
            <span className="spacer" />
            <DiffStat additions={f.additions} deletions={f.deletions} />
          </div>
          {f.isBinary ? (
            <div className="diff-binary">Binary file not shown.</div>
          ) : (
            <table className="diff-table">
              <tbody>
                {f.hunks.map((h, hi) => (
                  <HunkRows key={hi} header={h.header} lines={h.lines} />
                ))}
              </tbody>
            </table>
          )}
        </div>
      ))}
    </div>
  );
}

function HunkRows({
  header,
  lines,
}: {
  header: string;
  lines: DiffFile["hunks"][number]["lines"];
}) {
  return (
    <>
      <tr className="hunk">
        <td className="ln" />
        <td className="ln" />
        <td className="code">{hunkHeading(header)}</td>
      </tr>
      {lines.map((l, i) => (
        <tr key={i} className={`dl ${l.type}`}>
          <td className="ln">{l.oldNumber || ""}</td>
          <td className="ln">{l.newNumber || ""}</td>
          <td className="code">
            <span className="sign">
              {l.type === "add" ? "+" : l.type === "del" ? "−" : " "}
            </span>
            {l.content}
          </td>
        </tr>
      ))}
    </>
  );
}

// hunkHeading keeps the @@ range marker but trims any trailing code context so
// the header reads cleanly.
function hunkHeading(header: string): string {
  const end = header.lastIndexOf("@@");
  return end > 0 ? header.slice(0, end + 2) : header;
}
