import { notFound } from "next/navigation";
import Link from "next/link";

import { getBranches, getCommits } from "../../../../../../../lib/api";
import { getToken } from "../../../../../../../lib/session";
import {
  BrowseError,
  PathBreadcrumb,
  RepoHeader,
  fmtDate,
  firstLine,
  shortSha,
  splitRef,
} from "../../../../../../../components/repo";

// CommitsPage lists the commit log of a repository at a ref, optionally scoped
// to a path (the tree and blob pages link here with the browsed path, so a
// visitor sees "the history of this directory/file" — and the Code tab brings
// them back to the same place). The slug is a catch-all because branch names
// may contain slashes; splitRef resolves the ref/path boundary against the
// branch list.
export default async function CommitsPage({
  params,
}: {
  params: { project: string; repo: string; ref: string[] };
}) {
  // token may be absent — public repos are browsable anonymously.
  const token = await getToken();

  const slug = (params.ref ?? []).map(decodeURIComponent);
  const branchesRes = await getBranches(token, params.project, params.repo);
  const refNames = branchesRes.ok
    ? branchesRes.data.branches.map((b) => b.name)
    : [];
  const { ref, path } = splitRef(slug, refNames);

  const result = await getCommits(token, params.project, params.repo, ref, path, 50);
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

  const { repository: repo, commits } = result.data;

  return (
    <>
      <RepoHeader
        project={params.project}
        repo={params.repo}
        visibility={repo.visibility}
        refName={ref}
        path={path}
        active="commits"
      />
      <PathBreadcrumb
        project={params.project}
        repo={params.repo}
        refName={ref}
        path={path}
      />
      <div className="panel">
        <h2>
          Commits on {ref}
          <span className="tag">{commits.length}</span>
        </h2>
        {commits.length === 0 ? (
          <div className="empty">
            {path ? "No commits touch this path." : "No commits on this ref."}
          </div>
        ) : (
          commits.map((c) => (
            <Link
              className="row-item commit-row"
              key={c.sha}
              href={`/projects/${params.project}/repos/${params.repo}/commit/${c.sha}`}
            >
              <div className="pr-main">
                <span className="nm">{firstLine(c.message)}</span>
                <span className="sub">
                  {c.authorLogin || c.authorName} · {fmtDate(c.date)}
                </span>
              </div>
              <span className="spacer" />
              <span className="mono">{shortSha(c.sha)}</span>
            </Link>
          ))
        )}
      </div>
    </>
  );
}
