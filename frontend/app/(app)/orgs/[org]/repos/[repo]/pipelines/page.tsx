import Link from "next/link";
import { notFound } from "next/navigation";

import {
  getBranches,
  getPipelineRuns,
  getPipelines,
  type PipelineRun,
} from "../../../../../../lib/api";
import { getToken } from "../../../../../../lib/session";
import { BrowseError, RepoHeader, fmtDate, repoBase } from "../../../../../../components/repo";
import { RunStatusBadge, statusGlyph } from "../../../../../../components/pipelines";
import { RunWorkflowForm } from "./RunWorkflowForm";

// runHref links to a run's detail page, carrying the workflow path so the
// backend can resolve the run by (pipeline, number).
function runHref(
  org: string,
  repo: string,
  workflowPath: string,
  run: PipelineRun,
): string {
  return `${repoBase(org, repo)}/pipelines/runs/${run.runNumber}?workflow=${encodeURIComponent(
    workflowPath,
  )}`;
}

// PipelinesPage shows a repository's workflows and recent runs, and lets the
// user trigger a workflow manually.
export default async function PipelinesPage({
  params,
}: {
  params: { org: string; repo: string };
}) {
  const token = getToken();
  if (!token) notFound();

  const [pipelinesRes, runsRes, branchesRes] = await Promise.all([
    getPipelines(token, params.org, params.repo),
    getPipelineRuns(token, params.org, params.repo),
    getBranches(token, params.org, params.repo),
  ]);

  if (!pipelinesRes.ok) {
    if (pipelinesRes.status === 404) notFound();
    return (
      <BrowseError
        org={params.org}
        repo={params.repo}
        status={pipelinesRes.status}
        message={pipelinesRes.message}
      />
    );
  }

  const repo = pipelinesRes.data.repository;
  const pipelines = pipelinesRes.data.pipelines;
  const runs = runsRes.ok ? runsRes.data.runs : [];
  const branches = branchesRes.ok ? branchesRes.data.branches : [];
  const defaultBranch = repo.defaultBranch;

  // Map run number → workflow path so each recent-run row can link correctly.
  const workflowByRunId = new Map<string, string>();
  for (const p of pipelines) {
    if (p.lastRun) workflowByRunId.set(p.lastRun.id, p.workflowPath);
  }

  return (
    <>
      <RepoHeader
        org={params.org}
        repo={params.repo}
        visibility={repo.visibility}
        refName={defaultBranch}
        active="pipelines"
      />

      <RunWorkflowForm
        org={params.org}
        repo={params.repo}
        pipelines={pipelines}
        branches={branches}
        defaultBranch={defaultBranch}
      />

      <div className="panel">
        <h2>
          Workflows
          <span className="tag">{pipelines.length}</span>
        </h2>
        {pipelines.length === 0 ? (
          <div className="empty">
            No workflows found. Add a YAML file under{" "}
            <span className="mono">.github/workflows</span> to define a pipeline.
          </div>
        ) : (
          pipelines.map((p) => (
            <div className="row-item" key={p.workflowPath}>
              <span className="tree-icon">▷</span>
              <div className="pr-main">
                <span className="nm">{p.name}</span>
                <span className="sub mono">{p.workflowPath}</span>
              </div>
              <span className="spacer" />
              {p.lastRun ? (
                <Link
                  href={runHref(params.org, params.repo, p.workflowPath, p.lastRun)}
                  className="run-last"
                >
                  <RunStatusBadge status={p.lastRun.status} />
                  <span className="sub">
                    #{p.lastRun.runNumber} · {fmtDate(p.lastRun.createdAt)}
                  </span>
                </Link>
              ) : (
                <span className="sub">Never run</span>
              )}
            </div>
          ))
        )}
      </div>

      <div className="panel">
        <h2>
          Recent runs
          <span className="tag">{runs.length}</span>
        </h2>
        {runs.length === 0 ? (
          <div className="empty">No runs yet. Run a workflow to get started.</div>
        ) : (
          runs.map((run) => {
            const workflowPath = workflowByRunId.get(run.id) ?? run.workflowPath ?? "";
            const body = (
              <>
                <span className={`run-glyph ${run.status}`}>
                  {statusGlyph(run.status)}
                </span>
                <div className="pr-main">
                  <span className="nm">
                    Run #{run.runNumber} · {run.event}
                  </span>
                  <span className="sub">
                    {run.ref || "—"} · {fmtDate(run.createdAt)}
                  </span>
                </div>
                <span className="spacer" />
                <RunStatusBadge status={run.status} />
              </>
            );
            return workflowPath ? (
              <Link
                className="row-item"
                key={run.id}
                href={runHref(params.org, params.repo, workflowPath, run)}
              >
                {body}
              </Link>
            ) : (
              <div className="row-item" key={run.id}>
                {body}
              </div>
            );
          })
        )}
      </div>
    </>
  );
}
