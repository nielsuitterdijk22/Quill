import Link from "next/link";
import { notFound } from "next/navigation";

import { getContents, getCommits } from "../../../../lib/api";
import { getToken } from "../../../../lib/session";
import {
  BrowseError,
  DirView,
  RepoHeader,
  repoBase,
} from "../../../../components/repo";

// RepoHomePage is a repository's landing page: the file tree at the default
// branch root, a latest-commit strip, and a rendered README when present.
export default async function RepoHomePage({
  params,
}: {
  params: { org: string; repo: string };
}) {
  const token = getToken();
  if (!token) notFound();

  const result = await getContents(token, params.org, params.repo);
  if (!result.ok) {
    if (result.status === 404) notFound();
    return (
      <BrowseError
        org={params.org}
        repo={params.repo}
        status={result.status}
        message={result.message}
      />
    );
  }

  const { repository: repo, contents } = result.data;
  const ref = repo.defaultBranch;
  const entries = contents.entries ?? [];

  // Latest commit (for the strip) and a README (rendered below the tree) are
  // best-effort: failures degrade rather than break the page.
  const readmeEntry = entries.find(
    (e) => e.type === "file" && /^readme(\.md|\.txt)?$/i.test(e.name),
  );
  const [commitsRes, readmeRes] = await Promise.all([
    getCommits(token, params.org, params.repo, ref, "", 1),
    readmeEntry
      ? getContents(token, params.org, params.repo, readmeEntry.path, ref)
      : Promise.resolve(null),
  ]);
  const latest =
    commitsRes.ok && commitsRes.data.commits.length > 0
      ? commitsRes.data.commits[0]
      : null;
  const readme =
    readmeRes && readmeRes.ok && readmeRes.data.contents.file?.content
      ? readmeRes.data.contents.file
      : null;

  return (
    <>
      <RepoHeader
        org={params.org}
        repo={params.repo}
        visibility={repo.visibility}
        refName={ref}
        active="code"
      />

      {repo.description && <p className="subtle">{repo.description}</p>}

      <div className="repo-toolbar">
        <Link className="branch-pick" href={`${repoBase(params.org, params.repo)}/branches`}>
          <span className="ic">⎇</span> {ref}
        </Link>
        <span className="spacer" />
        <Link
          className="pill"
          href={`${repoBase(params.org, params.repo)}/commits/${encodeURIComponent(ref)}`}
        >
          commits
        </Link>
      </div>

      <DirView
        org={params.org}
        repo={params.repo}
        refName={ref}
        path=""
        entries={entries}
        latest={latest}
      />

      {readme && (
        <div className="panel readme">
          <h2>
            <span className="fn">{readme.name}</span>
          </h2>
          <div className="readme-body">
            <pre>{readme.content}</pre>
          </div>
        </div>
      )}
    </>
  );
}
