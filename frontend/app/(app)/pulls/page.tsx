import Link from "next/link";
import { notFound } from "next/navigation";

import { getMyPulls } from "../../lib/api";
import { getToken } from "../../lib/session";
import { fmtDate } from "../../components/repo";
import { DiffStat, PullStateBadge } from "../../components/pulls";

export default async function PullsOverviewPage({
  searchParams,
}: {
  searchParams: { state?: string };
}) {
  const token = await getToken();
  if (!token) notFound();

  const wantClosed = searchParams.state === "closed";
  const [openRes, closedRes] = await Promise.all([
    getMyPulls(token, { state: "open" }),
    getMyPulls(token, { state: "closed" }),
  ]);

  const active = wantClosed ? closedRes : openRes;

  return (
    <>
      <div className="top">
        <h1>Pull requests</h1>
      </div>

      <div className="repo-toolbar">
        <div className="state-tabs">
          <Link className={wantClosed ? "" : "active"} href="/pulls">
            <span className="ic">◍</span>{" "}
            {openRes.ok ? openRes.data.pulls.length : 0} Open
          </Link>
          <Link
            className={wantClosed ? "active" : ""}
            href="/pulls?state=closed"
          >
            <span className="ic">✓</span>{" "}
            {closedRes.ok ? closedRes.data.pulls.length : 0} Closed
          </Link>
        </div>
        <span className="spacer" />
      </div>

      <div className="panel">
        {!active.ok ? (
          <div className="empty">
            {active.message || "Could not load pull requests."}
          </div>
        ) : active.data.pulls.length === 0 ? (
          <div className="empty">
            {wantClosed
              ? "No closed pull requests across your repositories."
              : "No open pull requests across your repositories."}
          </div>
        ) : (
          active.data.pulls.map((rp) => (
            <Link
              className="row-item pr-row"
              key={`${rp.projectSlug}/${rp.repoSlug}#${rp.pull.number}`}
              href={`/${encodeURIComponent(rp.projectSlug)}/${encodeURIComponent(rp.repoSlug)}/pulls/${rp.pull.number}`}
            >
              <PullStateBadge pull={rp.pull} />
              <div className="pr-main">
                <span className="nm">{rp.pull.title}</span>
                <span className="sub">
                  {rp.projectSlug}/{rp.repoSlug} #{rp.pull.number} ·{" "}
                  {rp.pull.author?.login ?? "unknown"} · updated{" "}
                  {fmtDate(rp.pull.updatedAt)}
                </span>
              </div>
              <span className="spacer" />
              <DiffStat
                additions={rp.pull.additions}
                deletions={rp.pull.deletions}
              />
              {rp.pull.comments > 0 && (
                <span className="pr-comments">💬 {rp.pull.comments}</span>
              )}
            </Link>
          ))
        )}
      </div>
    </>
  );
}
