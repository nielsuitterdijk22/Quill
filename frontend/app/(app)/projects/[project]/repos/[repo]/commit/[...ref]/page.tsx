import { notFound } from "next/navigation";

import { getCommit } from "../../../../../../../lib/api";
import { getToken } from "../../../../../../../lib/session";
import {
  BrowseError,
  RepoHeader,
  fmtDate,
  firstLine,
  shortSha,
} from "../../../../../../../components/repo";
import { DiffStat, DiffView } from "../../../../../../../components/pulls";

// CommitPage shows a single commit: its subject and author, the rest of the
// message, and the full diff of what it changed. The SHA arrives as a catch-all
// slug to share the route shape with the other ref-based pages; commit SHAs
// never contain slashes, so the segments simply rejoin into the SHA.
export default async function CommitPage({
  params,
}: {
  params: { project: string; repo: string; ref: string[] };
}) {
  const token = await getToken();
  if (!token) notFound();

  const sha = (params.ref ?? []).map(decodeURIComponent).join("/");
  if (!sha) notFound();

  const result = await getCommit(token, params.project, params.repo, sha);
  if (!result.ok) {
    if (result.status === 404) notFound();
    return (
      <BrowseError
        project={params.project}
        repo={params.repo}
        status={result.status}
        message={result.message}
      />
    );
  }

  const { repository: repo, commit, files } = result.data;
  const additions = files.reduce((n, f) => n + f.additions, 0);
  const deletions = files.reduce((n, f) => n + f.deletions, 0);
  // The subject is the first line; everything after the blank line is the body.
  const body = commit.message.slice(firstLine(commit.message).length).trim();

  return (
    <>
      <RepoHeader
        project={params.project}
        repo={params.repo}
        visibility={repo.visibility}
        refName={repo.defaultBranch}
        active="commits"
      />

      <div className="pr-title-row">
        <h1 className="pr-title">{firstLine(commit.message)}</h1>
      </div>
      <div className="pr-meta">
        <span className="subtle">
          <b>{commit.authorLogin || commit.authorName}</b> committed ·{" "}
          {fmtDate(commit.date)}
        </span>
        <span className="spacer" />
        <span className="mono">{shortSha(commit.sha)}</span>
        <DiffStat additions={additions} deletions={deletions} />
      </div>

      {body && (
        <div className="panel">
          <pre>{body}</pre>
        </div>
      )}

      <DiffView files={files} />
    </>
  );
}
