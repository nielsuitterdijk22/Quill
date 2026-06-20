import { notFound, redirect } from "next/navigation";

import { getBranches, getContents } from "../../../../../../../lib/api";
import { getToken } from "../../../../../../../lib/session";
import {
  BrowseError,
  CodeView,
  PathBreadcrumb,
  RepoHeader,
  humanBytes,
  splitRef,
  treeHref,
} from "../../../../../../../components/repo";

// BlobPage shows a single file's contents at a ref. The ref and path arrive as
// one catch-all slug (branch names may contain slashes); splitRef resolves the
// boundary against the branch list. Binary or oversized files render a notice
// instead of inline text.
export default async function BlobPage({
  params,
}: {
  params: { project: string; repo: string; slug: string[] };
}) {
  const token = await getToken();
  if (!token) notFound();

  const slug = (params.slug ?? []).map(decodeURIComponent);
  const branchesRes = await getBranches(token, params.project, params.repo);
  const refNames = branchesRes.ok
    ? branchesRes.data.branches.map((b) => b.name)
    : [];
  const { ref, path } = splitRef(slug, refNames);
  if (!path) notFound();

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
  if (contents.type === "dir") {
    redirect(treeHref(params.project, params.repo, ref, path));
  }
  if (!contents.file) notFound();
  const file = contents.file;

  return (
    <>
      <RepoHeader
        project={params.project}
        repo={params.repo}
        visibility={repo.visibility}
        refName={ref}
        active="code"
      />
      <PathBreadcrumb
        project={params.project}
        repo={params.repo}
        refName={ref}
        path={path}
      />
      <div className="file-bar">
        <span className="mono">{file.name}</span>
        <span className="spacer" style={{ flex: 1 }} />
        <span className="subtle">{humanBytes(file.size)}</span>
      </div>
      {file.tooLarge ? (
        <div className="panel attached">
          <div className="empty">
            This file is too large to display ({humanBytes(file.size)}).
          </div>
        </div>
      ) : file.isBinary ? (
        <div className="panel attached">
          <div className="empty">Binary file not shown.</div>
        </div>
      ) : (
        <CodeView content={file.content ?? ""} />
      )}
    </>
  );
}
