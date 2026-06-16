import Link from "next/link";
import { notFound } from "next/navigation";

import {
  getPipelines,
  listReposByOrg,
  type PipelineRun,
} from "../../lib/api";
import { getDefaultOrg } from "../../lib/orgs";
import { getToken } from "../../lib/session";
import { fmtDate } from "../../components/repo";
import { RunStatusBadge } from "../../components/pipelines";

// PipelineRow is one workflow flattened with the repository it belongs to, so the
// overview can list pipelines (not repositories) while still naming their source.
type PipelineRow = {
  repoSlug: string;
  repoName: string;
  workflowPath: string;
  name: string;
  lastRun?: PipelineRun;
};

// PipelinesOverviewPage lists every pipeline (workflow) across the repositories in
// the user's default org, each with its source repository and latest CI status.
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

  // Fetch each repo's workflows and flatten them into a single pipeline list.
  const perRepo = await Promise.all(
    repos.map(async (repo) => {
      const res = await getPipelines(token, org, repo.slug);
      const pipelines = res.ok ? res.data.pipelines : [];
      return pipelines.map<PipelineRow>((p) => ({
        repoSlug: repo.slug,
        repoName: repo.name,
        workflowPath: p.workflowPath,
        name: p.name,
        lastRun: p.lastRun,
      }));
    }),
  );

  // Most recently active pipelines first; never-run pipelines fall to the end.
  const rows = perRepo.flat().sort((a, b) => {
    const at = a.lastRun?.createdAt ?? "";
    const bt = b.lastRun?.createdAt ?? "";
    return bt.localeCompare(at);
  });

  return (
    <>
      <div className="top">
        <h1>Pipelines in {org}</h1>
      </div>

      <div className="panel">
        <h2>
          Pipelines
          <span className="tag">{rows.length}</span>
        </h2>
        {rows.length === 0 ? (
          <div className="empty">
            No pipelines yet. Add a workflow under{" "}
            <span className="mono">.github/workflows</span> in a repository to
            define one.
          </div>
        ) : (
          rows.map((row) => (
            <Link
              className="row-item"
              key={`${row.repoSlug}:${row.workflowPath}`}
              href={`/orgs/${org}/repos/${row.repoSlug}/pipelines`}
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
