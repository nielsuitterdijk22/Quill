import Link from "next/link";
import { notFound } from "next/navigation";

import {
  getPipelines,
  listReposByOrg,
  type PipelineSummary,
} from "../../lib/api";
import { getDefaultOrg } from "../../lib/orgs";
import { getToken } from "../../lib/session";
import { fmtDate } from "../../components/repo";
import { RunStatusBadge } from "../../components/pipelines";

// latestRun returns the most recent last-run across a repo's workflows, so the
// overview can show a single status per repository.
function latestRun(pipelines: PipelineSummary[]): PipelineSummary["lastRun"] {
  let latest: PipelineSummary["lastRun"];
  for (const p of pipelines) {
    if (!p.lastRun) continue;
    if (!latest || p.lastRun.createdAt > latest.createdAt) latest = p.lastRun;
  }
  return latest;
}

// PipelinesOverviewPage lists the repositories in the user's default org with a
// CI status summary, linking each to its pipelines tab.
export default async function PipelinesOverviewPage() {
  const token = getToken();
  if (!token) notFound();

  const org = await getDefaultOrg();
  if (!org) {
    return (
      <>
        <div className="top">
          <h1>Pipelines</h1>
        </div>
        <div className="panel">
          <div className="empty">
            Create an organization and a repository to start running pipelines.
          </div>
        </div>
      </>
    );
  }

  const repos = await listReposByOrg(token, org);

  // Fetch each repo's workflows so we can show its latest CI status. Repos
  // without git or without workflows simply show "No pipelines".
  const summaries = await Promise.all(
    repos.map(async (repo) => {
      const res = await getPipelines(token, org, repo.slug);
      const pipelines = res.ok ? res.data.pipelines : [];
      return { repo, pipelines, last: latestRun(pipelines) };
    }),
  );

  return (
    <>
      <div className="top">
        <h1>Pipelines in {org}</h1>
      </div>

      <div className="panel">
        <h2>
          Repositories
          <span className="tag">{repos.length}</span>
        </h2>
        {repos.length === 0 ? (
          <div className="empty">No repositories yet. Create one to add CI.</div>
        ) : (
          summaries.map(({ repo, pipelines, last }) => (
            <Link
              className="row-item"
              key={repo.id}
              href={`/orgs/${org}/repos/${repo.slug}/pipelines`}
            >
              <span className="tree-icon">▷</span>
              <div className="pr-main">
                <span className="nm">{repo.name}</span>
                <span className="sub">
                  {pipelines.length === 0
                    ? "No pipelines"
                    : `${pipelines.length} workflow${pipelines.length === 1 ? "" : "s"}`}
                </span>
              </div>
              <span className="spacer" />
              {last ? (
                <>
                  <span className="sub">
                    #{last.runNumber} · {fmtDate(last.createdAt)}
                  </span>
                  <RunStatusBadge status={last.status} />
                </>
              ) : (
                <span className="sub">—</span>
              )}
            </Link>
          ))
        )}
      </div>
    </>
  );
}
