import Link from "next/link";
import { notFound } from "next/navigation";

import { getBranches } from "../../../../../lib/api";
import { getToken } from "../../../../../lib/session";
import {
  BrowseError,
  RepoHeader,
  fmtDate,
  firstLine,
  shortSha,
  treeHref,
} from "../../../../../components/repo";

// BranchesPage lists a repository's git branches, each linking to the file tree
// at that branch.
export default async function BranchesPage({
  params,
}: {
  params: { org: string; repo: string };
}) {
  const token = getToken();
  if (!token) notFound();

  const result = await getBranches(token, params.org, params.repo);
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

  const { repository: repo, defaultBranch, branches } = result.data;

  return (
    <>
      <RepoHeader
        org={params.org}
        repo={params.repo}
        visibility={repo.visibility}
        refName={defaultBranch}
        active="branches"
      />

      <div className="panel">
        <h2>
          Branches
          <span className="tag">{branches.length}</span>
        </h2>
        {branches.map((b) => (
          <Link
            className="row-item"
            key={b.name}
            href={treeHref(params.org, params.repo, b.name)}
          >
            <span className="tree-icon">⎇</span>
            <span className="nm">{b.name}</span>
            {b.name === defaultBranch && <span className="badge accent">default</span>}
            {b.protected && <span className="badge amber">protected</span>}
            <span className="spacer" />
            <span className="sub mono">{shortSha(b.commitSha)}</span>
            <span className="sub">{firstLine(b.commitMessage)}</span>
            <span className="sub">{fmtDate(b.commitDate)}</span>
          </Link>
        ))}
      </div>
    </>
  );
}
