import Link from "next/link";
import { notFound } from "next/navigation";

import { listIssues, getRepo } from "../../../../../../lib/api";
import { getToken } from "../../../../../../lib/session";
import { BrowseError, RepoHeader, fmtDate, repoBase } from "../../../../../../components/repo";

export default async function IssuesPage({
  params,
  searchParams,
}: {
  params: { project: string; repo: string };
  searchParams: { state?: string };
}) {
  const token = getToken();
  if (!token) notFound();

  const wantClosed = searchParams.state === "closed";
  const state = wantClosed ? "closed" : "open";

  const [repoRes, issuesRes, closedRes, openRes] = await Promise.all([
    getRepo(token, params.project, params.repo),
    listIssues(token, params.project, params.repo, state),
    listIssues(token, params.project, params.repo, "closed"),
    listIssues(token, params.project, params.repo, "open"),
  ]);

  if (!repoRes.ok) {
    if (repoRes.status === 404) notFound();
    return (
      <BrowseError
        project={params.project}
        repo={params.repo}
        status={repoRes.status}
        message={repoRes.message}
      />
    );
  }
  if (!issuesRes.ok) {
    if (issuesRes.status === 404) notFound();
    return (
      <BrowseError
        project={params.project}
        repo={params.repo}
        status={issuesRes.status}
        message={issuesRes.message}
      />
    );
  }

  const repo = repoRes.data;
  const issues = issuesRes.data.issues;
  const openCount = openRes.ok ? openRes.data.issues.length : 0;
  const closedCount = closedRes.ok ? closedRes.data.issues.length : 0;
  const base = repoBase(params.project, params.repo);

  return (
    <>
      <RepoHeader
        project={params.project}
        repo={params.repo}
        visibility={repo.visibility}
        refName={repo.defaultBranch}
        active="issues"
      />

      <div className="repo-toolbar">
        <div className="state-tabs">
          <Link className={wantClosed ? "" : "active"} href={`${base}/issues`}>
            <span className="ic">◍</span> {openCount} Open
          </Link>
          <Link
            className={wantClosed ? "active" : ""}
            href={`${base}/issues?state=closed`}
          >
            <span className="ic">✓</span> {closedCount} Closed
          </Link>
        </div>
        <span className="spacer" />
        <Link className="btn primary" href={`${base}/issues/new`}>
          New issue
        </Link>
      </div>

      <div className="panel">
        {issues.length === 0 ? (
          <div className="empty">
            {wantClosed
              ? "No closed issues yet."
              : "No open issues. Create one to track work or report a bug."}
          </div>
        ) : (
          issues.map((issue) => (
            <Link
              className="row-item pr-row"
              key={issue.number}
              href={`${base}/issues/${issue.number}`}
            >
              <span className={`issue-state-ic ${issue.state}`}>
                {issue.state === "open" ? "◍" : "✓"}
              </span>
              <div className="pr-main">
                <span className="nm">{issue.title}</span>
                <span className="sub">
                  #{issue.number} opened by {issue.author?.login ?? "unknown"} ·{" "}
                  {fmtDate(issue.createdAt)}
                  {issue.labels.length > 0 && (
                    <> · {issue.labels.map((l) => l.name).join(", ")}</>
                  )}
                </span>
              </div>
              <span className="spacer" />
              {issue.comments > 0 && (
                <span className="pr-comments">💬 {issue.comments}</span>
              )}
            </Link>
          ))
        )}
      </div>
    </>
  );
}
