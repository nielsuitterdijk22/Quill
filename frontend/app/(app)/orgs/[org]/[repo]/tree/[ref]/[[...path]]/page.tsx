import { notFound, redirect } from "next/navigation";

import { getContents, getCommits } from "../../../../../../../lib/api";
import { getToken } from "../../../../../../../lib/session";
import {
  BrowseError,
  DirView,
  PathBreadcrumb,
  RepoHeader,
  blobHref,
} from "../../../../../../../components/repo";

// TreePage browses a directory at a given ref and path. If the path resolves to
// a file it redirects to the blob view.
export default async function TreePage({
  params,
}: {
  params: { org: string; repo: string; ref: string; path?: string[] };
}) {
  const token = getToken();
  if (!token) notFound();

  const ref = params.ref;
  const path = (params.path ?? []).join("/");

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

  return (
    <>
      <RepoHeader
        org={params.org}
        repo={params.repo}
        visibility={repo.visibility}
        refName={ref}
        active="code"
      />
      <PathBreadcrumb
        org={params.org}
        repo={params.repo}
        refName={ref}
        path={path}
      />
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
