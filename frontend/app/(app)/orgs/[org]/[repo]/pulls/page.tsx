import Link from "next/link";
import { notFound } from "next/navigation";

import { getPulls } from "../../../../../lib/api";
import { getToken } from "../../../../../lib/session";
import {
  BrowseError,
  RepoHeader,
  fmtDate,
  repoBase,
} from "../../../../../components/repo";
import { DiffStat, PullStateBadge } from "../../../../../components/pulls";

// PullsPage lists a repository's pull requests with Open/Closed filters.
export default async function PullsPage({
  params,
  searchParams,
}: {
  params: { org: string; repo: string };
  searchParams: { state?: string };
}) {
  const token = getToken();
  if (!token) notFound();

  const wantClosed = searchParams.state === "closed";
  const [openRes, closedRes] = await Promise.all([
    getPulls(token, params.org, params.repo, "open"),
    getPulls(token, params.org, params.repo, "closed"),
  ]);

  const active = wantClosed ? closedRes : openRes;
  if (!active.ok) {
    if (active.status === 404) notFound();
    return (
      <BrowseError
        org={params.org}
        repo={params.repo}
        status={active.status}
        message={active.message}
      />
    );
  }

  const repo = active.data.repository;
  const pulls = active.data.pulls;
  const openCount = openRes.ok ? openRes.data.pulls.length : 0;
  const closedCount = closedRes.ok ? closedRes.data.pulls.length : 0;
  const base = repoBase(params.org, params.repo);

  return (
    <>
      <RepoHeader
        org={params.org}
        repo={params.repo}
        visibility={repo.visibility}
        refName={repo.defaultBranch}
        active="pulls"
      />

      <div className="repo-toolbar">
        <div className="state-tabs">
          <Link className={wantClosed ? "" : "active"} href={`${base}/pulls`}>
            <span className="ic">◍</span> {openCount} Open
          </Link>
          <Link
            className={wantClosed ? "active" : ""}
            href={`${base}/pulls?state=closed`}
          >
            <span className="ic">✓</span> {closedCount} Closed
          </Link>
        </div>
        <span className="spacer" />
        <Link className="btn primary" href={`${base}/pulls/new`}>
          New pull request
        </Link>
      </div>

      <div className="panel">
        {pulls.length === 0 ? (
          <div className="empty">
            {wantClosed
              ? "No closed pull requests yet."
              : "No open pull requests. Open one to propose a change."}
          </div>
        ) : (
          pulls.map((p) => (
            <Link
              className="row-item pr-row"
              key={p.number}
              href={`${base}/pulls/${p.number}`}
            >
              <PullStateBadge pull={p} />
              <div className="pr-main">
                <span className="nm">{p.title}</span>
                <span className="sub">
                  #{p.number} opened by {p.author?.login ?? "unknown"} ·{" "}
                  {fmtDate(p.createdAt)} · {p.head.ref} → {p.base.ref}
                </span>
              </div>
              <span className="spacer" />
              <DiffStat additions={p.additions} deletions={p.deletions} />
              {p.comments > 0 && (
                <span className="pr-comments">💬 {p.comments}</span>
              )}
            </Link>
          ))
        )}
      </div>
    </>
  );
}
