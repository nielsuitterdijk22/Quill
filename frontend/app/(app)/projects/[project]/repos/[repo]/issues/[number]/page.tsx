import Link from "next/link";
import { notFound } from "next/navigation";

import { getIssue, getRepo } from "../../../../../../../lib/api";
import { getToken } from "../../../../../../../lib/session";
import {
  BrowseError,
  RepoHeader,
  fmtDate,
  repoBase,
} from "../../../../../../../components/repo";
import { IssueActions } from "./IssueActions";
import { IssueCommentForm } from "./IssueCommentForm";

export default async function IssueDetailPage({
  params,
}: {
  params: { project: string; repo: string; number: string };
}) {
  const token = await getToken();
  if (!token) notFound();

  const number = Number.parseInt(params.number, 10);
  if (!Number.isFinite(number) || number <= 0) notFound();

  const [repoRes, res] = await Promise.all([
    getRepo(token, params.project, params.repo),
    getIssue(token, params.project, params.repo, number),
  ]);

  if (!res.ok) {
    if (res.status === 404) notFound();
    return (
      <BrowseError
        project={params.project}
        repo={params.repo}
        status={res.status}
        message={res.message}
      />
    );
  }

  const repo = repoRes.ok ? repoRes.data : null;
  const { issue, comments } = res.data;
  const base = repoBase(params.project, params.repo);

  return (
    <>
      <RepoHeader
        project={params.project}
        repo={params.repo}
        visibility={repo?.visibility ?? "private"}
        refName={repo?.defaultBranch ?? ""}
        active="issues"
      />

      <div className="panel issue-hero">
        <div className="issue-hero-main">
          <span className={`issue-state-ic ${issue.state}`}>
            {issue.state === "open" ? "◍" : "✓"}
          </span>
          <div>
            <h1>
              {issue.title}{" "}
              <span className="muted">#{issue.number}</span>
            </h1>
            <div className="run-meta">
              <span className={`badge ${issue.state === "open" ? "success" : "neutral"}`}>
                {issue.state}
              </span>
              <span>
                {issue.author?.login ?? "unknown"} opened {fmtDate(issue.createdAt)}
              </span>
              <span>{issue.comments} comment{issue.comments !== 1 ? "s" : ""}</span>
              {issue.labels.map((l) => (
                <span
                  key={l.name}
                  className="label-badge"
                  style={{ background: `#${l.color}` }}
                >
                  {l.name}
                </span>
              ))}
            </div>
          </div>
        </div>
        <div className="run-hero-actions">
          <IssueActions
            project={params.project}
            repo={params.repo}
            number={issue.number}
            state={issue.state}
          />
          <Link className="btn" href={`${base}/issues`}>
            ← All issues
          </Link>
        </div>
      </div>

      {issue.body && (
        <div className="panel">
          <div className="pr-author-row">
            <strong>{issue.author?.login ?? "unknown"}</strong>
            <span className="muted">{fmtDate(issue.createdAt)}</span>
          </div>
          <div className="readme-body">
            <pre style={{ whiteSpace: "pre-wrap" }}>{issue.body}</pre>
          </div>
        </div>
      )}

      {comments.map((c) => (
        <div className="panel" key={c.id}>
          <div className="pr-author-row">
            <strong>{c.author?.login ?? "unknown"}</strong>
            <span className="muted">{fmtDate(c.createdAt)}</span>
          </div>
          <div className="readme-body">
            <pre style={{ whiteSpace: "pre-wrap" }}>{c.body}</pre>
          </div>
        </div>
      ))}

      <IssueCommentForm
        project={params.project}
        repo={params.repo}
        number={issue.number}
      />
    </>
  );
}
