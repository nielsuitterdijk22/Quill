import Link from "next/link";

import { getPipelines, listReposByProject, getMyProjects, type PipelineRun } from "../../lib/api";
import { getToken } from "../../lib/session";
import { fmtDate } from "../../components/repo";
import { RunStatusBadge } from "../../components/pipelines";

type PipelineRow = {
  projectSlug: string;
  repoSlug: string;
  repoName: string;
  workflowPath: string;
  name: string;
  lastRun?: PipelineRun;
};

// PipelinesOverviewPage lists every pipeline across all of the user's repos.
export default async function PipelinesOverviewPage() {
  const token = await getToken();
  const projects = token ? await getMyProjects(token) : [];

  const perProject = token
    ? await Promise.all(
        projects.map(async (project) => {
          const repos = await listReposByProject(token, project.slug);
          const perRepo = await Promise.all(
            repos.map(async (repo) => {
              const res = await getPipelines(token, project.slug, repo.slug);
              const pipelines = res.ok ? res.data.pipelines : [];
              return pipelines.map<PipelineRow>((p) => ({
                projectSlug: project.slug,
                repoSlug: repo.slug,
                repoName: repo.name,
                workflowPath: p.workflowPath,
                name: p.name,
                lastRun: p.lastRun,
              }));
            }),
          );
          return perRepo.flat();
        }),
      )
    : [];

  const rows = perProject
    .flat()
    .sort((a, b) => (b.lastRun?.createdAt ?? "").localeCompare(a.lastRun?.createdAt ?? ""));

  return (
    <>
      <div className="top">
        <h1>Pipelines</h1>
      </div>

      <div className="panel">
        <h2>
          Pipelines
          <span className="tag">{rows.length}</span>
        </h2>
        {rows.length === 0 ? (
          <div className="empty">
            No pipelines yet. Add a workflow under{" "}
            <span className="mono">.github/workflows</span> in a repository.
          </div>
        ) : (
          rows.map((row) => (
            <Link
              className="row-item"
              key={`${row.projectSlug}:${row.repoSlug}:${row.workflowPath}`}
              href={`/${encodeURIComponent(row.projectSlug)}/${encodeURIComponent(row.repoSlug)}/pipelines`}
            >
              <span className="tree-icon">◇</span>
              <div className="pr-main">
                <span className="nm">{row.name}</span>
                <span className="sub run-row-meta">
                  <span className="mono">{row.repoName}</span>
                  <span className="mono">{row.workflowPath}</span>
                </span>
              </div>
              <span className="spacer" />
              {row.lastRun ? (
                <>
                  <span className="sub">
                    #{row.lastRun.runNumber} · {fmtDate(row.lastRun.createdAt)}
                  </span>
                  <RunStatusBadge status={row.lastRun.status} />
                </>
              ) : (
                <span className="sub">Never run</span>
              )}
            </Link>
          ))
        )}
      </div>
    </>
  );
}
