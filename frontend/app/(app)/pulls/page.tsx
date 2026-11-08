import Link from "next/link";
import { notFound } from "next/navigation";

import { getMyPulls } from "../../lib/api";
import { getToken } from "../../lib/session";
import { resolveCurrentProject } from "../../lib/projects";
import { fmtDate } from "../../components/repo";
import { DiffStat, PullStateBadge } from "../../components/pulls";

export default async function PullsOverviewPage({
  searchParams,
}: {
  searchParams: { state?: string; scope?: string };
}) {
  const token = await getToken();
  if (!token) notFound();

  const wantClosed = searchParams.state === "closed";
  // The overview follows the sidebar's current project, like the other
  // cross-cutting views. "All" is an explicit escape hatch for multi-project
  // users; single-project users always stay scoped.
  const resolved = await resolveCurrentProject(token);
  const hasMultiple = (resolved?.projects.length ?? 0) > 1;
  const scopeAll = searchParams.scope === "all" || !resolved;
  const project = scopeAll ? undefined : resolved.current.slug;
  // Tab hrefs keep the current scope so toggling Open/Closed doesn't reset it.
  const scopeQuery = scopeAll ? "&scope=all" : "";

  const [openRes, closedRes] = await Promise.all([
    getMyPulls(token, { state: "open", project }),
    getMyPulls(token, { state: "closed", project }),
  ]);

  const active = wantClosed ? closedRes : openRes;

  return (
    <>
      <div className="top">
        <h1>Pull requests</h1>
      </div>

      <div className="repo-toolbar">
        <div className="state-tabs">
          <Link className={wantClosed ? "" : "active"} href={`/pulls${scopeAll ? "?scope=all" : ""}`}>
            <span className="ic">◍</span>{" "}
            {openRes.ok ? openRes.data.pulls.length : 0} Open
          </Link>
          <Link
            className={wantClosed ? "active" : ""}
            href={`/pulls?state=closed${scopeQuery}`}
          >
            <span className="ic">✓</span>{" "}
            {closedRes.ok ? closedRes.data.pulls.length : 0} Closed
          </Link>
        </div>
        {hasMultiple && (
          <span className="subtle project-scope-hint">
            {scopeAll ? (
              <Link href="/pulls">Just this project</Link>
            ) : (
              <Link href="/pulls?scope=all">All projects</Link>
            )}
          </span>
        )}
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
              ? scopeAll
                ? "No closed pull requests across your projects."
                : `No closed pull requests in ${resolved!.current.name}. Switch to “All projects” to see your other workspaces.`
              : scopeAll
                ? "No open pull requests across your projects."
                : `No open pull requests in ${resolved!.current.name}. Switch to “All projects” to see your other workspaces.`}
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
