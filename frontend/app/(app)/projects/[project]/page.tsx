import Link from "next/link";
import { notFound } from "next/navigation";

import { getReposByProject } from "../../../lib/api";
import { getToken } from "../../../lib/session";
import { VisibilityBadge } from "../../../components/repo";

// ProjectDetailPage lists the repositories in an project. Membership is enforced
// by the backend; a 403 renders a no-access notice, a 404 is a real not-found.
export default async function ProjectDetailPage({
  params,
}: {
  params: { project: string };
}) {
  const token = getToken();
  if (!token) notFound();

  const result = await getReposByProject(token, params.project);
  if (!result.ok) {
    if (result.status === 404) notFound();
    return (
      <>
        <div className="crumbs">
          <Link href="/projects">Projects</Link> <span>/</span>{" "}
          <span>{params.project}</span>
        </div>
        <h1>{params.project}</h1>
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
        <Link href="/projects">Projects</Link> <span>/</span>{" "}
        <span>{project.slug}</span>
      </div>

      <div className="top">
        <h1>{project.name}</h1>
        <Link className="btn primary" href={`/projects/${project.slug}/repos/new`}>
          ＋ New repository
        </Link>
      </div>

      {project.description && <p className="subtle">{project.description}</p>}

      <div className="panel">
        <h2>
          Repositories
          <span className="tag">{repos.length}</span>
        </h2>
        {repos.length === 0 ? (
          <div className="empty">
            No repositories yet. Create one — it is initialised in Forgejo with
            a default branch and a README.
          </div>
        ) : (
          repos.map((r) => (
            <Link
              className="row-item"
              key={r.id}
              href={`/projects/${project.slug}/repos/${r.slug}`}
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
