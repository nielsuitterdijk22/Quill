import { notFound } from "next/navigation";

import { getPull, getPullComments, getPullDiff } from "../../../../../../lib/api";
import { getToken } from "../../../../../../lib/session";
import {
  BrowseError,
  RepoHeader,
  fmtDate,
  shortSha,
} from "../../../../../../components/repo";
import { DiffStat, DiffView, PullStateBadge } from "../../../../../../components/pulls";
import { CommentForm } from "./CommentForm";
import { MergeBox } from "./MergeBox";

// PullDetailPage shows a pull request's header, merge controls, conversation,
// and the parsed diff of its changes.
export default async function PullDetailPage({
  params,
}: {
  params: { org: string; repo: string; number: string };
}) {
  const token = getToken();
  if (!token) notFound();

  const number = Number(params.number);
  if (!Number.isInteger(number) || number <= 0) notFound();

  const prRes = await getPull(token, params.org, params.repo, number);
  if (!prRes.ok) {
    if (prRes.status === 404) notFound();
    return (
      <BrowseError
        org={params.org}
        repo={params.repo}
        status={prRes.status}
        message={prRes.message}
      />
    );
  }

  const { repository: repo, pull } = prRes.data;
  const [commentsRes, diffRes] = await Promise.all([
    getPullComments(token, params.org, params.repo, number),
    getPullDiff(token, params.org, params.repo, number),
  ]);
  const comments = commentsRes.ok ? commentsRes.data.comments : [];
  const files = diffRes.ok ? diffRes.data.files : [];
  const isOpen = pull.state === "open" && !pull.merged;

  return (
    <>
      <RepoHeader
        org={params.org}
        repo={params.repo}
        visibility={repo.visibility}
        refName={repo.defaultBranch}
        active="pulls"
      />

      <div className="pr-title-row">
        <h1 className="pr-title">
          {pull.title} <span className="pr-num">#{pull.number}</span>
        </h1>
      </div>
      <div className="pr-meta">
        <PullStateBadge pull={pull} />
        <span className="subtle">
          <b>{pull.author?.login ?? "unknown"}</b> wants to merge{" "}
          <span className="mono">{pull.head.ref}</span> into{" "}
          <span className="mono">{pull.base.ref}</span> ·{" "}
          {pull.changedFiles} file{pull.changedFiles === 1 ? "" : "s"} changed
        </span>
        <span className="spacer" />
        <DiffStat additions={pull.additions} deletions={pull.deletions} />
      </div>

      {pull.merged ? (
        <div className="panel merge-box merged">
          <div className="merge-head">
            <span className="merge-dot done" />
            <strong>
              Merged by {pull.mergedBy?.login ?? pull.author?.login ?? "unknown"}
              {pull.mergeCommitSha
                ? ` · ${shortSha(pull.mergeCommitSha)}`
                : ""}
            </strong>
          </div>
        </div>
      ) : !isOpen ? (
        <div className="panel merge-box">
          <div className="merge-head">
            <span className="merge-dot warn" />
            <strong>This pull request is closed.</strong>
          </div>
        </div>
      ) : (
        <MergeBox
          org={params.org}
          repo={params.repo}
          number={number}
          mergeable={pull.mergeable}
        />
      )}

      <h2 className="section">Conversation</h2>
      <div className="convo">
        <div className="pr-comment">
          <div className="pr-comment-head">
            <b>{pull.author?.login ?? "unknown"}</b>
            <span className="subtle"> opened · {fmtDate(pull.createdAt)}</span>
          </div>
          <div className="pr-comment-body">
            {pull.body ? (
              <pre>{pull.body}</pre>
            ) : (
              <span className="subtle">No description provided.</span>
            )}
          </div>
        </div>

        {comments.map((c) => (
          <div className="pr-comment" key={c.id}>
            <div className="pr-comment-head">
              <b>{c.author?.login ?? "unknown"}</b>
              <span className="subtle"> commented · {fmtDate(c.createdAt)}</span>
            </div>
            <div className="pr-comment-body">
              <pre>{c.body}</pre>
            </div>
          </div>
        ))}

        {isOpen && (
          <CommentForm org={params.org} repo={params.repo} number={number} />
        )}
      </div>

      <h2 className="section">
        Changed files <span className="tag">{pull.changedFiles}</span>
      </h2>
      {diffRes.ok ? (
        <DiffView files={files} />
      ) : (
        <div className="banner">Could not load the diff for this pull request.</div>
      )}
    </>
  );
}
