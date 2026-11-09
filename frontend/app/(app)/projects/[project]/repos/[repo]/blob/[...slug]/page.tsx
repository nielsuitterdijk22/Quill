import { notFound, redirect } from "next/navigation";

import { getBranches, getCommits, getContents, renderMarkdown } from "../../../../../../../lib/api";
import { getToken } from "../../../../../../../lib/session";
import {
  BrowseError,
  CodeView,
  fmtDate,
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
  searchParams,
}: {
  params: { project: string; repo: string; slug: string[] };
  searchParams: { view?: string };
}) {
  // token may be absent — public repos are browsable anonymously.
  const token = await getToken();

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

  // Markdown files render as formatted markup by default (Forgejo's markup
  // engine, same as the README), with a toggle back to the raw source.
  const isMarkdown = /\.(md|markdown)$/i.test(file.name);
  const wantSource = searchParams?.view === "source";
  const markdownHtml =
    isMarkdown && !wantSource
      ? await renderMarkdown(token, params.project, params.repo, file.content ?? "")
      : null;

  // Fetch the most recent commit touching this file so the header can show
  // when it was last edited.
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
        {latest?.date && (
          <span className="subtle">Last edited {fmtDate(latest.date)}</span>
        )}
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
        <>
          {isMarkdown && (
            <div className="blob-view-tabs">
              <a className={wantSource ? "" : "active"} href={`?${new URLSearchParams(ref ? { ref } : {}).toString()}`}>
                Preview
              </a>
              <a className={wantSource ? "active" : ""} href={`?${new URLSearchParams({ ...(ref ? { ref } : {}), view: "source" }).toString()}`}>
                Code
              </a>
            </div>
          )}
          {markdownHtml ? (
            <div
              className="panel attached markdown-body blob-markdown"
              dangerouslySetInnerHTML={{ __html: markdownHtml }}
            />
          ) : (
            <CodeView content={file.content ?? ""} />
          )}
        </>
      )}
    </>
  );
}
