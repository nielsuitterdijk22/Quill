import { notFound, redirect } from "next/navigation";

import {
  getBranches,
  getContents,
  getCommits,
  getMeta,
  renderMarkdown,
} from "../../../../../../../lib/api";
import { getToken } from "../../../../../../../lib/session";
import { CloneButton } from "../../../../../../../components/CloneButton";
import {
  BrowseError,
  cloneHttpUrl,
  DirView,
  PathBreadcrumb,
  ReadmeView,
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
  params: { project: string; repo: string; slug: string[] };
}) {
  const token = await getToken();
  if (!token) notFound();

  const slug = (params.slug ?? []).map(decodeURIComponent);
  const [branchesRes, meta] = await Promise.all([
    getBranches(token, params.project, params.repo),
    getMeta(),
  ]);
  const refNames = branchesRes.ok
    ? branchesRes.data.branches.map((b) => b.name)
    : [];
  const { ref, path } = splitRef(slug, refNames);

  const result = await getContents(token, params.project, params.repo, path, ref);
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

  const { repository: repo, contents } = result.data;
  if (contents.type === "file") {
    redirect(blobHref(params.project, params.repo, ref, path));
  }

  const latestRes = await getCommits(
    token,
    params.project,
    params.repo,
    ref,
    path,
    1,
  );
  const latest =
    latestRes.ok && latestRes.data.commits.length > 0
      ? latestRes.data.commits[0]
      : null;

  // Render the README for this directory, mirroring the repo home page.
  const entries = contents.entries ?? [];
  const readmeEntry = entries.find(
    (e) => e.type === "file" && /^readme(\.md|\.txt)?$/i.test(e.name),
  );
  const readmeRes = readmeEntry
    ? await getContents(token, params.project, params.repo, readmeEntry.path, ref)
    : null;
  const readme =
    readmeRes && readmeRes.ok && readmeRes.data.contents.file?.content
      ? readmeRes.data.contents.file
      : null;
  const readmeHtml =
    readme && /\.md$/i.test(readme.name)
      ? await renderMarkdown(token, params.project, params.repo, readme.content ?? "")
      : null;

  const httpUrl = cloneHttpUrl(
    meta?.forgejo?.publicUrl,
    repo.forgejoOwner,
    repo.forgejoName,
    params.project,
    params.repo,
  );

  return (
    <>
      <RepoHeader
        project={params.project}
        repo={params.repo}
        visibility={repo.visibility}
        refName={ref}
        active="code"
      />
      <div className="repo-toolbar">
        <BranchSelector
          project={params.project}
          repo={params.repo}
          selectedBranch={ref}
          branches={refNames}
          path={path}
        />
        <span className="spacer" />
        <CloneButton httpUrl={httpUrl} />
      </div>
      {path && (
        <PathBreadcrumb
          project={params.project}
          repo={params.repo}
          refName={ref}
          path={path}
        />
      )}
      <DirView
        project={params.project}
        repo={params.repo}
        refName={ref}
        path={path}
        entries={entries}
        latest={latest}
      />
      {readme && (
        <ReadmeView name={readme.name} html={readmeHtml} raw={readme.content ?? ""} />
      )}
    </>
  );
}
