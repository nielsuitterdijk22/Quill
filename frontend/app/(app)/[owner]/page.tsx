import Link from "next/link";
import { notFound } from "next/navigation";

import { getReposByProject } from "../../lib/api";
import { getToken } from "../../lib/session";
import { VisibilityBadge } from "../../components/repo";

// OwnerNamespacePage is the landing page for a user or org namespace at
// /{owner}. It renders the same repository list as the project detail page but
// uses short /{owner}/{repo} links throughout.
export default async function OwnerNamespacePage({
  params,
}: {
  params: { owner: string };
}) {
  const token = await getToken();
  if (!token) notFound();

  const result = await getReposByProject(token, params.owner);
  if (!result.ok) {
    if (result.status === 404) notFound();
    return (
      <>
        <div className="crumbs">
          <span>{params.owner}</span>
        </div>
        <h1>{params.owner}</h1>
        <div className="banner">
          {result.status === 403
            ? "You are not a member of this project."
            : result.message}
        </div>
      </>
    );
  }

  const { project, repositories: repos } = result.data;

  return (
    <>
      <div className="crumbs">
        <span>{project.slug}</span>
      </div>

      <div className="top">
        <h1>{project.name}</h1>
        <div className="top-actions">
          <Link
            className="btn ghost"
            href={`/projects/${project.slug}/settings`}
          >
            ⚙ Settings
          </Link>
          <Link
            className="btn primary"
            href={`/projects/${project.slug}/repos/new`}
          >
            ＋ New repository
          </Link>
        </div>
      </div>

      {project.description && <p className="subtle">{project.description}</p>}

      <div className="panel">
        <h2>
          Repositories
          <span className="tag">{repos.length}</span>
        </h2>
        {repos.length === 0 ? (
          <div className="empty">
            No repositories yet. Create one to get started.
          </div>
        ) : (
          repos.map((r) => (
            <Link
              className="row-item"
              key={r.id}
              href={`/${encodeURIComponent(project.slug)}/${encodeURIComponent(r.slug)}`}
            >
              <span className="tree-icon">▤</span>
              <span className="nm">{r.name}</span>
              <span className="sub">· {r.slug}</span>
              <span className="spacer" />
              <VisibilityBadge visibility={r.visibility} />
            </Link>
          ))
        )}
      </div>
    </>
  );
}
