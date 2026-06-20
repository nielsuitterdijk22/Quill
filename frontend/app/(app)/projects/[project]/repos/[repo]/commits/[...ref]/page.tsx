import { notFound } from "next/navigation";
import Link from "next/link";

import { getCommits } from "../../../../../../../lib/api";
import { getToken } from "../../../../../../../lib/session";
import {
  BrowseError,
  RepoHeader,
  fmtDate,
  firstLine,
  shortSha,
} from "../../../../../../../components/repo";

// CommitsPage lists the commit log of a repository at a ref. The ref arrives as a
// catch-all slug so branch names containing slashes resolve correctly; the whole
// slug is the ref (this route never carries a path).
export default async function CommitsPage({
  params,
}: {
  params: { project: string; repo: string; ref: string[] };
}) {
  const token = await getToken();
  if (!token) notFound();

  const ref = (params.ref ?? []).map(decodeURIComponent).join("/");
  const result = await getCommits(token, params.project, params.repo, ref, "", 50);
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
        active="commits"
      />

      <div className="panel">
        <h2>
          Commits on {ref}
          <span className="tag">{commits.length}</span>
        </h2>
        {commits.length === 0 ? (
          <div className="empty">No commits on this ref.</div>
        ) : (
          commits.map((c) => (
            <div className="row-item" key={c.sha}>
              <div style={{ display: "flex", flexDirection: "column", gap: 2 }}>
                <span className="nm">{firstLine(c.message)}</span>
                <span className="sub">
                  {c.authorLogin || c.authorName} · {fmtDate(c.date)}
                </span>
              </div>
              <span className="spacer" />
              <Link
                className="mono"
                href={`/projects/${params.project}/repos/${params.repo}/commit/${c.sha}`}
              >
                {shortSha(c.sha)}
              </Link>
            </div>
          ))
        )}
      </div>
    </>
  );
}
