import Link from "next/link";
import { notFound } from "next/navigation";

import { getToken } from "../../lib/session";
import { listReposByProject } from "../../lib/api";
import { resolveCurrentProject } from "../../lib/projects";

// ReposPage lists the repositories of the current project (chosen via the
// sidebar project switcher). The current project is resolved from the cookie,
// falling back to the user's first project.
export default async function ReposPage() {
  const token = getToken();
  if (!token) notFound();

  const resolved = await resolveCurrentProject(token);
  if (!resolved) {
    return (
      <>
        <div className="top">
          <h1>Repositories</h1>
        </div>
        <div className="panel">
          <div className="empty">
            Create a project to start adding repositories.
          </div>
        </div>
      </>
    );
  }

  const project = resolved.current;
  const repos = await listReposByProject(token, project.slug);

  return (
    <>
      <div className="top">
        <h1>Repositories in {project.name}</h1>
        <Link
          className="btn primary"
          href={`/projects/${project.slug}/repos/new`}
        >
          + New Repository
        </Link>
      </div>

      <div className="panel">
        <h2>
          All Repositories
          <span className="tag">{repos.length}</span>
        </h2>
        {repos.length === 0 ? (
          <div className="empty">No repositories yet. Create one :D</div>
        ) : (
          repos.map((o) => (
            <Link
              className="row-item"
              key={o.id}
              href={`/projects/${project.slug}/repos/${o.slug}`}
            >
              <span className="tree-icon dir">◆</span>
              <span className="nm">{o.name}</span>
              <span className="sub">· {o.slug}</span>
              <span className="spacer" />
            </Link>
          ))
        )}
      </div>
    </>
  );
}
