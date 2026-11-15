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
import { Markdown } from "../../../../../../../components/Markdown";
import { EditableDescription } from "../../../../../../../components/EditableDescription";
import { editIssueAction } from "./actions";
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

      <div className="convo">
        <EditableDescription
          action={editIssueAction.bind(null, params.project, params.repo, issue.number)}
          title={issue.title}
          body={issue.body ?? ""}
          head={
            <>
              <b>{issue.author?.login ?? "unknown"}</b>
              <span className="subtle"> opened · {fmtDate(issue.createdAt)}</span>
            </>
          }
        >
          {issue.body ? (
            <Markdown token={token} project={params.project} repo={params.repo} text={issue.body} />
          ) : (
            <span className="subtle">No description provided.</span>
          )}
        </EditableDescription>

        {comments.map((c) => (
          <div className="pr-comment" key={c.id}>
            <div className="pr-comment-head">
              <b>{c.author?.login ?? "unknown"}</b>
              <span className="subtle"> commented · {fmtDate(c.createdAt)}</span>
            </div>
            <div className="pr-comment-body">
              <Markdown token={token} project={params.project} repo={params.repo} text={c.body} />
            </div>
          </div>
        ))}
      </div>

      <IssueCommentForm
        project={params.project}
        repo={params.repo}
        number={issue.number}
      />
    </>
  );
}
