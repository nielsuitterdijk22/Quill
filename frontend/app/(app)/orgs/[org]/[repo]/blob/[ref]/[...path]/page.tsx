import { notFound, redirect } from "next/navigation";

import { getContents } from "../../../../../../../lib/api";
import { getToken } from "../../../../../../../lib/session";
import {
  BrowseError,
  CodeView,
  PathBreadcrumb,
  RepoHeader,
  humanBytes,
  treeHref,
} from "../../../../../../../components/repo";

// BlobPage shows a single file's contents at a ref. Binary or oversized files
// render a notice instead of inline text.
export default async function BlobPage({
  params,
}: {
  params: { org: string; repo: string; ref: string; path?: string[] };
}) {
  const token = getToken();
  if (!token) notFound();

  const ref = params.ref;
  const path = (params.path ?? []).join("/");
  if (!path) notFound();

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
  if (contents.type === "dir") {
    redirect(treeHref(params.org, params.repo, ref, path));
  }
  if (!contents.file) notFound();
  const file = contents.file;

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
