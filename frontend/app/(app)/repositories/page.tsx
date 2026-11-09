import { EmptyState } from "@/components/EmptyState";
import { RepositoryBrowser } from "./RepositoryBrowser";
import Link from "next/link";

import { getToken } from "../../lib/session";
import { getMyProjects, listReposByProject } from "../../lib/api";
import type { Repo, MyProject } from "../../lib/api";
import { ImportGitHubButton } from "./ImportGitHubButton";

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
        <div className="top-actions">
          <ImportGitHubButton projects={projects} />
          <Link className="btn primary" href={newRepoHref}>
            + New Repository
          </Link>
        </div>
      </div>

      {rows.length === 0 ? (
        <div className="panel">
          <h2>
            All Repositories
            <span className="tag">{rows.length}</span>
          </h2>
          <EmptyState
            title="No repositories yet"
            body="Create your first repository, or import the repos you already have on GitHub."
            cta={{ href: newRepoHref, label: "New repository" }}
          />
        </div>
      ) : (
        <RepositoryBrowser
          newRepoHref={newRepoHref}
          rows={rows.map(({ repo, project }) => ({
            repo,
            projectSlug: project.slug,
            projectName: project.name,
            showProject: hasOrgProjects,
          }))}
        />
      )}
    </>
  );
}
