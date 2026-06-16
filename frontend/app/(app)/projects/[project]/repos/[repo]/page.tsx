import Link from "next/link";
import { notFound } from "next/navigation";

import {
  getBranches,
  getContents,
  getCommits,
  getMeta,
  getRepo,
  renderMarkdown,
} from "../../../../../lib/api";
import { getToken } from "../../../../../lib/session";
import { CloneButton } from "../../../../../components/CloneButton";
import { BranchSelector } from "../../../../../components/BranchSelector";
import {
  BrowseError,
  cloneHttpUrl,
  commitsHref,
  DirView,
  ReadmeView,
  RepoHeader,
  repoBase,
  VisibilityBadge,
} from "../../../../../components/repo";

export default async function RepoHomePage({
  params,
}: {
  params: { project: string; repo: string };
}) {
  const token = getToken();
  if (!token) notFound();

  const result = await getContents(token, params.project, params.repo);

  // 409 means the git repo exists in Forgejo but has no commits yet. Show
  // setup instructions instead of an error.
  if (!result.ok && result.status === 409) {
    const [repoRes, meta] = await Promise.all([
      getRepo(token, params.project, params.repo),
      getMeta(),
    ]);
    if (!repoRes.ok) {
      if (repoRes.status === 404) notFound();
      return (
        <BrowseError
          project={params.project}
          repo={params.repo}
          status={repoRes.status}
          message={repoRes.message}
        />
      );
    }
    const repo = repoRes.data;
    const forgejoPublicUrl =
      meta?.forgejo?.publicUrl ?? "http://localhost:2222";
    const owner = repo.forgejoOwner ?? params.project;
    const name = repo.forgejoName ?? params.repo;
    const httpUrl = `${forgejoPublicUrl}/${owner}/${name}.git`;
    const base = repoBase(params.project, params.repo);

    return (
      <>
        <div className="crumbs">
          <Link href="/projects">Projects</Link> <span>/</span>{" "}
          <Link href={`/projects/${encodeURIComponent(params.project)}`}>
            {params.project}
          </Link>{" "}
          <span>/</span> <span>{params.repo}</span>
        </div>
        <div className="top">
          <h1>
            {params.project}/<b>{params.repo}</b>
          </h1>
          <VisibilityBadge visibility={repo.visibility} />
        </div>
        <nav className="rtabs">
          <Link href={base} className="active">
            Code
          </Link>
          <Link href={`${base}/pulls`}>Pull requests</Link>
          <Link href={`${base}/settings`}>Settings</Link>
        </nav>

        <div className="panel">
          <h2>Get started</h2>
          <div className="empty-repo-body">
            <p className="subtle">
              This repository is empty. Push your first commit to get started.
            </p>
            <div className="clone-section">
              <span className="clone-label">Clone</span>
              <code className="clone-url">{httpUrl}</code>
              <CloneButton httpUrl={httpUrl} />
            </div>
            <pre className="setup-cmds">{`git clone ${httpUrl}
cd ${name}
# … add files …
git add .
git commit -m "Initial commit"
git push origin ${repo.defaultBranch || "main"}`}</pre>
            <p className="subtle">Or push an existing repository:</p>
            <pre className="setup-cmds">{`git remote add origin ${httpUrl}
git push -u origin ${repo.defaultBranch || "main"}`}</pre>
          </div>
        </div>
      </>
    );
  }

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
  const ref = repo.defaultBranch;
  const entries = contents.entries ?? [];

  const readmeEntry = entries.find(
    (e) => e.type === "file" && /^readme(\.md|\.txt)?$/i.test(e.name),
  );
  const [branchesRes, commitsRes, readmeRes, meta] = await Promise.all([
    getBranches(token, params.project, params.repo),
    getCommits(token, params.project, params.repo, ref, "", 1),
    readmeEntry
      ? getContents(token, params.project, params.repo, readmeEntry.path, ref)
      : Promise.resolve(null),
    getMeta(),
  ]);
  const refNames = branchesRes.ok
    ? branchesRes.data.branches.map((b) => b.name)
    : [ref];
  const latest =
    commitsRes.ok && commitsRes.data.commits.length > 0
      ? commitsRes.data.commits[0]
      : null;
  const readme =
    readmeRes && readmeRes.ok && readmeRes.data.contents.file?.content
      ? readmeRes.data.contents.file
      : null;
  // Render markdown READMEs as HTML via Forgejo's markup engine; plain-text
  // READMEs (.txt) are shown verbatim.
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

      {repo.description && <p className="subtle">{repo.description}</p>}

      <div className="repo-toolbar">
        <BranchSelector
          project={params.project}
          repo={params.repo}
          selectedBranch={ref}
          branches={refNames}
          path=""
        />
        <span className="spacer" />
        <Link className="pill" href={commitsHref(params.project, params.repo, ref)}>
          commits
        </Link>
        <CloneButton httpUrl={httpUrl} />
      </div>

      <DirView
        project={params.project}
        repo={params.repo}
        refName={ref}
        path=""
        entries={entries}
        latest={latest}
      />

      {readme && (
        <ReadmeView name={readme.name} html={readmeHtml} raw={readme.content ?? ""} />
      )}
    </>
  );
}
