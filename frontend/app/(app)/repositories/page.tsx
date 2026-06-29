import Link from "next/link";

import { getToken } from "../../lib/session";
import { getMyProjects, listReposByProject } from "../../lib/api";
import type { Repo, MyProject } from "../../lib/api";

// ReposPage lists all repositories across the user's projects.
// For individual users (only a personal project) repos are shown flat.
// For org users each repo shows its project badge.
export default async function ReposPage() {
  const token = await getToken();
  const projects = token ? await getMyProjects(token) : [];
  const hasOrgProjects = projects.some((p) => !p.isPersonal);

  type RepoWithProject = { repo: Repo; project: MyProject };

  const perProject = token
    ? await Promise.all(
        projects.map(async (p) => {
          const repos = await listReposByProject(token, p.slug);
          return repos.map<RepoWithProject>((r) => ({ repo: r, project: p }));
        }),
      )
    : [];

  const rows = perProject.flat();

  const personalProject = projects.find((p) => p.isPersonal);
  const newRepoHref = hasOrgProjects
    ? "/projects"
    : personalProject
      ? `/projects/${personalProject.slug}/repos/new`
      : "/repositories/new";

  return (
    <>
      <div className="top">
        <h1>Repositories</h1>
        <Link className="btn primary" href={newRepoHref}>
          + New Repository
        </Link>
      </div>

      <div className="panel">
        <h2>
          All Repositories
          <span className="tag">{rows.length}</span>
        </h2>
        {rows.length === 0 ? (
          <div className="empty">No repositories yet. Create one above.</div>
        ) : (
          rows.map(({ repo, project }) => (
            <Link
              className="row-item"
              key={repo.id}
              href={`/${encodeURIComponent(project.slug)}/${encodeURIComponent(repo.slug)}`}
            >
              <span className="tree-icon dir">◆</span>
              <div className="pr-main">
                <span className="nm">{repo.name}</span>
                {hasOrgProjects && (
                  <span className="sub">{project.name}</span>
                )}
              </div>
              <span className="spacer" />
              {repo.visibility !== "public" && (
                <span className="tag">{repo.visibility}</span>
              )}
            </Link>
          ))
        )}
      </div>
    </>
  );
}
