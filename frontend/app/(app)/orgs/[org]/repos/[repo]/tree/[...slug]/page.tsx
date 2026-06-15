import { notFound, redirect } from "next/navigation";

import {
  getBranches,
  getContents,
  getCommits,
  getMeta,
} from "../../../../../../../lib/api";
import { getToken } from "../../../../../../../lib/session";
import { CloneButton } from "../../../../../../../components/CloneButton";
import {
  BrowseError,
  cloneHttpUrl,
  DirView,
  PathBreadcrumb,
  RepoHeader,
  blobHref,
  splitRef,
} from "../../../../../../../components/repo";
import { BranchSelector } from "../../../../../../../components/BranchSelector";

// TreePage browses a directory at a given ref and path. The ref and path arrive
// as one catch-all slug because branch names may contain slashes; splitRef
// resolves the boundary against the repository's branch list. If the path
// resolves to a file it redirects to the blob view.
export default async function TreePage({
  params,
}: {
  params: { org: string; repo: string; slug: string[] };
}) {
  const token = getToken();
  if (!token) notFound();

  const slug = (params.slug ?? []).map(decodeURIComponent);
  const [branchesRes, meta] = await Promise.all([
    getBranches(token, params.org, params.repo),
    getMeta(),
  ]);
  const refNames = branchesRes.ok
    ? branchesRes.data.branches.map((b) => b.name)
    : [];
  const { ref, path } = splitRef(slug, refNames);

  const result = await getContents(token, params.org, params.repo, path, ref);
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
  if (contents.type === "file") {
    redirect(blobHref(params.org, params.repo, ref, path));
  }

  const latestRes = await getCommits(
    token,
    params.org,
    params.repo,
    ref,
    path,
    1,
  );
  const latest =
    latestRes.ok && latestRes.data.commits.length > 0
      ? latestRes.data.commits[0]
      : null;
  const httpUrl = cloneHttpUrl(
    meta?.forgejo?.publicUrl,
    repo.forgejoOwner,
    repo.forgejoName,
    params.org,
    params.repo,
  );

  return (
    <>
      <RepoHeader
        org={params.org}
        repo={params.repo}
        visibility={repo.visibility}
        refName={ref}
        active="code"
      />
      <div className="repo-toolbar">
        <BranchSelector
          org={params.org}
          repo={params.repo}
          selectedBranch={ref}
          branches={refNames}
          path={path}
        />
        <span className="spacer" />
        <CloneButton httpUrl={httpUrl} />
      </div>
      <DirView
        org={params.org}
        repo={params.repo}
        refName={ref}
        path={path}
        entries={contents.entries ?? []}
        latest={latest}
      />
    </>
  );
}
