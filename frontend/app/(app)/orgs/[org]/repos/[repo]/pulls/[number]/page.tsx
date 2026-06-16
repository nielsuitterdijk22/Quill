import Link from "next/link";
import { notFound } from "next/navigation";

import {
  getLineComments,
  getPull,
  getPullComments,
  getPullCommits,
  getPullDiff,
  getPullReviews,
  type PolicyGate,
} from "../../../../../../../lib/api";
import { getToken, getSession } from "../../../../../../../lib/session";
import {
  BrowseError,
  RepoHeader,
  fmtDate,
  firstLine,
  repoBase,
  shortSha,
} from "../../../../../../../components/repo";
import {
  DiffStat,
  PullStateBadge,
  ReviewStateBadge,
} from "../../../../../../../components/pulls";
import { CommentForm } from "./CommentForm";
import { DiffWithComments } from "./DiffWithComments";
import { MergeBox } from "./MergeBox";
import { ReviewForm } from "./ReviewForm";

const NO_GATE: PolicyGate = {
  applies: false,
  requiredApprovals: 0,
  approvals: 0,
  changesRequested: 0,
  blocked: false,
};

type Tab = "conversation" | "commits" | "files";

function asTab(value: string | undefined): Tab {
  if (value === "commits") return "commits";
  if (value === "files") return "files";
  return "conversation";
}

// PullDetailPage shows a pull request's header and merge controls, then one of
// three tabs: the Conversation, the Commits it contains, and the Files changed
// (where reviewers can leave line comments).
export default async function PullDetailPage({
  params,
  searchParams,
}: {
  params: { org: string; repo: string; number: string };
  searchParams: { tab?: string };
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
  const tab = asTab(searchParams.tab);

  const [commentsRes, diffRes, reviewsRes, commitsRes, lineCommentsRes] =
    await Promise.all([
      getPullComments(token, params.org, params.repo, number),
      getPullDiff(token, params.org, params.repo, number),
      getPullReviews(token, params.org, params.repo, number),
      getPullCommits(token, params.org, params.repo, number),
      getLineComments(token, params.org, params.repo, number),
    ]);
  const currentUser = await getSession();
  const comments = commentsRes.ok ? commentsRes.data.comments : [];
  const files = diffRes.ok ? diffRes.data.files : [];
  const allReviews = reviewsRes.ok ? reviewsRes.data.reviews : [];
  const gate = reviewsRes.ok ? reviewsRes.data.gate : NO_GATE;
  const commits = commitsRes.ok ? commitsRes.data.commits : [];
  const lineComments = lineCommentsRes.ok ? lineCommentsRes.data.comments : [];
  const isOpen = pull.state === "open" && !pull.merged;
  // Forgejo rejects reviews on your own pull request, so we hide the review form
  // for the author and show an explanation instead.
  const isAuthor =
    !!currentUser && currentUser.username === pull.author?.login;

  // Line comments are carried as empty-bodied COMMENT reviews; hide those from
  // the conversation so they don't show up as blank "reviewed" entries.
  const reviews = allReviews.filter(
    (rv) => !(rv.state === "COMMENT" && !rv.body?.trim()),
  );

  // Group line-anchored review comments by file so the conversation can surface
  // them (they otherwise only appear in the Files changed tab).
  const lineCommentGroups = Array.from(
    lineComments
      .reduce((map, c) => {
        const arr = map.get(c.path) ?? [];
        arr.push(c);
        map.set(c.path, arr);
        return map;
      }, new Map<string, typeof lineComments>())
      .entries(),
  ).map(([path, items]) => ({
    path,
    items: [...items].sort((a, b) => a.line - b.line),
  }));

  const base = `${repoBase(params.org, params.repo)}/pulls/${number}`;
  const tabHref = (t: Tab) =>
    t === "conversation" ? base : `${base}?tab=${t}`;
  const tabs: { key: Tab; label: string; count: number }[] = [
    { key: "conversation", label: "Conversation", count: comments.length },
    { key: "commits", label: "Commits", count: commits.length },
    { key: "files", label: "Files changed", count: pull.changedFiles },
  ];

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
          <span className="mono">{pull.base.ref}</span> · {pull.changedFiles}{" "}
          file{pull.changedFiles === 1 ? "" : "s"} changed
        </span>
        <span className="spacer" />
        <DiffStat additions={pull.additions} deletions={pull.deletions} />
      </div>

      {pull.merged ? (
        <div className="panel merge-box merged">
          <div className="merge-head">
            <span className="merge-dot done" />
            <strong>
              Merged by{" "}
              {pull.mergedBy?.login ?? pull.author?.login ?? "unknown"}
              {pull.mergeCommitSha ? ` · ${shortSha(pull.mergeCommitSha)}` : ""}
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
          gate={gate}
        />
      )}

      <nav className="pr-tabs">
        {tabs.map((t) => (
          <Link
            key={t.key}
            href={tabHref(t.key)}
            className={t.key === tab ? "active" : ""}
          >
            {t.label} <span className="tag">{t.count}</span>
          </Link>
        ))}
      </nav>

      {tab === "conversation" && (
        <div className="convo">
          <div className="pr-comment">
            <div className="pr-comment-head">
              <b>{pull.author?.login ?? "unknown"}</b>
              <span className="subtle">
                {" "}
                opened · {fmtDate(pull.createdAt)}
              </span>
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
                <span className="subtle">
                  {" "}
                  commented · {fmtDate(c.createdAt)}
                </span>
              </div>
              <div className="pr-comment-body">
                <pre>{c.body}</pre>
              </div>
            </div>
          ))}

          {reviews.map((rv) => (
            <div className="pr-comment review" key={rv.id}>
              <div className="pr-comment-head">
                <b>{rv.author?.login ?? "unknown"}</b>
                <span className="subtle">
                  {" "}
                  reviewed · {fmtDate(rv.submittedAt)}
                </span>
                <span className="spacer" />
                {rv.dismissed ? (
                  <span className="badge">Dismissed</span>
                ) : rv.stale ? (
                  <span className="badge">Stale</span>
                ) : null}
                <ReviewStateBadge state={rv.state} />
              </div>
              {rv.body && (
                <div className="pr-comment-body">
                  <pre>{rv.body}</pre>
                </div>
              )}
            </div>
          ))}

          {lineCommentGroups.length > 0 && (
            <div className="pr-comment review">
              <div className="pr-comment-head">
                <b>Code review comments</b>
                <span className="subtle">
                  {" "}
                  · {lineComments.length} on the diff
                </span>
                <span className="spacer" />
                <Link href={`${base}?tab=files`} className="subtle">
                  View in Files changed
                </Link>
              </div>
              <div className="pr-comment-body line-comment-groups">
                {lineCommentGroups.map((g) => (
                  <div className="line-comment-group" key={g.path}>
                    <div className="lc-file mono">{g.path}</div>
                    {g.items.map((c) => (
                      <div className="line-comment" key={c.id}>
                        <div className="line-comment-head">
                          <b>{c.author ?? "unknown"}</b>
                          <span className="subtle">
                            {" "}
                            · line {c.line} · {fmtDate(c.createdAt)}
                          </span>
                        </div>
                        <div className="line-comment-body">
                          <pre>{c.body}</pre>
                        </div>
                      </div>
                    ))}
                  </div>
                ))}
              </div>
            </div>
          )}

          {isOpen && (
            <>
              <CommentForm
                org={params.org}
                repo={params.repo}
                number={number}
              />
              {isAuthor ? (
                <div className="review-form review-self">
                  <div className="review-form-head">
                    <strong>Review changes</strong>
                    <span className="subtle">
                      You can&rsquo;t review your own pull request. Another member
                      needs to approve it to satisfy the branch policy.
                    </span>
                  </div>
                </div>
              ) : (
                <ReviewForm
                  org={params.org}
                  repo={params.repo}
                  number={number}
                />
              )}
            </>
          )}
        </div>
      )}

      {tab === "commits" && (
        <div className="panel">
          {commits.length === 0 ? (
            <div className="empty">No commits in this pull request.</div>
          ) : (
            commits.map((c) => (
              <div className="row-item" key={c.sha}>
                <div
                  style={{ display: "flex", flexDirection: "column", gap: 2 }}
                >
                  <span className="nm">{firstLine(c.message)}</span>
                  <span className="sub">
                    {c.authorLogin || c.authorName} · {fmtDate(c.date)}
                  </span>
                </div>
                <span className="spacer" />
                <span className="mono">{shortSha(c.sha)}</span>
              </div>
            ))
          )}
        </div>
      )}

      {tab === "files" &&
        (diffRes.ok ? (
          <DiffWithComments
            org={params.org}
            repo={params.repo}
            number={number}
            files={files}
            comments={lineComments}
          />
        ) : (
          <div className="banner">
            Could not load the diff for this pull request.
          </div>
        ))}
    </>
  );
}
