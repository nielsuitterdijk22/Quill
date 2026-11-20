import { RepoChip } from "../../../../../../components/RepoChip";
import Link from "next/link";
import { notFound } from "next/navigation";

import {
  getBranches,
  getEnvironments,
  getPipelineRuns,
  getPipelines,
  type PipelineRun,
} from "../../../../../../lib/api";
import { getToken } from "../../../../../../lib/session";
import { BrowseError, RepoHeader, blobHref, fmtDate, repoBase } from "../../../../../../components/repo";
import { RunStatusBadge, durationText, statusGlyph } from "../../../../../../components/pipelines";
import { RunWorkflowForm } from "./RunWorkflowForm";
import { AutoRefresh } from "./AutoRefresh";

// runHref links to a run's detail page, carrying the workflow path so the
// backend can resolve the run by (pipeline, number).
function runHref(
  project: string,
  repo: string,
  workflowPath: string,
  run: PipelineRun,
): string {
  return `${repoBase(project, repo)}/pipelines/runs/${run.runNumber}?workflow=${encodeURIComponent(
    workflowPath,
  )}`;
}

function shortSha(sha: string): string {
  return sha ? sha.slice(0, 7) : "";
}

function workflowName(
  workflows: Map<string, string>,
  workflowPath: string,
): string {
  return workflows.get(workflowPath) ?? workflowPath.split("/").pop() ?? "Workflow";
}

// PipelinesPage shows a repository's workflows and recent runs, and lets the
// user trigger a workflow manually.
export default async function PipelinesPage({
  params,
  searchParams,
}: {
  params: { project: string; repo: string };
  searchParams: { workflow?: string };
}) {
  const workflowFilter = (searchParams.workflow ?? "").trim();
  const token = await getToken();
  if (!token) notFound();

  const [pipelinesRes, runsRes, branchesRes, environmentsRes] = await Promise.all([
    getPipelines(token, params.project, params.repo),
    getPipelineRuns(token, params.project, params.repo),
    getBranches(token, params.project, params.repo),
    getEnvironments(token, params.project),
  ]);

  if (!pipelinesRes.ok) {
    if (pipelinesRes.status === 404) notFound();
    return (
      <BrowseError
        project={params.project}
        repo={params.repo}
        status={pipelinesRes.status}
        message={pipelinesRes.message}
      />
    );
  }

  const repo = pipelinesRes.data.repository;
  const pipelines = pipelinesRes.data.pipelines;
  const allRuns = runsRes.ok ? runsRes.data.runs : [];
  const branches = branchesRes.ok ? branchesRes.data.branches : [];
  const environments = environmentsRes.ok ? environmentsRes.data.environments : [];
  const defaultBranch = repo.defaultBranch;

  // Map known run ids to workflow paths so recent-run rows can link correctly.
  const workflowByRunId = new Map<string, string>();
  const workflowNameByPath = new Map<string, string>();
  for (const p of pipelines) {
    workflowNameByPath.set(p.workflowPath, p.name);
    if (p.lastRun) workflowByRunId.set(p.lastRun.id, p.workflowPath);
  }

  // ?workflow=<path> narrows the run list to one pipeline (linked from the
  // cross-repo Pipelines overview).
  const runs = workflowFilter
    ? allRuns.filter(
        (r) => (workflowByRunId.get(r.id) ?? r.workflowPath ?? "") === workflowFilter,
      )
    : allRuns;

  return (
    <>
      <RepoHeader
        project={params.project}
        repo={params.repo}
        visibility={repo.visibility}
        refName={defaultBranch}
        active="pipelines"
      />

      <AutoRefresh active={allRuns.some((r) => r.status === "pending" || r.status === "running")} />

      <RunWorkflowForm
        project={params.project}
        repo={params.repo}
        pipelines={pipelines}
        branches={branches}
        defaultBranch={defaultBranch}
        environments={environments}
      />

      <div className="panel">
        <h2>
          <span>
            {workflowFilter
              ? `Runs of ${workflowName(workflowNameByPath, workflowFilter)} in `
              : "Recent runs in "}
            <RepoChip project={params.project} repo={params.repo} showProject={false} />
          </span>
          <span className="tag">{runs.length}</span>
        </h2>
        {workflowFilter && (
          <div className="sub run-filter-note">
            Filtered to <span className="mono">{workflowFilter}</span> ·{" "}
            <Link href={`${repoBase(params.project, params.repo)}/pipelines`}>Show all runs</Link>
          </div>
        )}
        {runs.length === 0 ? (
          <div className="empty">No runs yet. Run a workflow to get started.</div>
        ) : (
          runs.map((run) => {
            const workflowPath = workflowByRunId.get(run.id) ?? run.workflowPath ?? "";
            const body = (
              <>
                {run.status === "running" || run.status === "pending" ? (
                  <span className="run-glyph">
                    <span className="run-spinner" aria-label={run.status} />
                  </span>
                ) : (
                  <span className={`run-glyph ${run.status}`}>{statusGlyph(run.status)}</span>
                )}
                <div className="pr-main">
                  <span className="nm">
                    {workflowPath
                      ? workflowName(workflowNameByPath, workflowPath)
                      : `Run #${run.runNumber}`}
                  </span>
                  <span className="sub run-row-meta">
                    <span>#{run.runNumber}</span>
                    <span>{run.event}</span>
                    <span>{run.ref || "—"}</span>
                    {run.commitSha && (
                      <span className="mono">{shortSha(run.commitSha)}</span>
                    )}
                    <span>{fmtDate(run.createdAt)}</span>
                    {durationText(run.startedAt, run.finishedAt) && (
                      <span>{durationText(run.startedAt, run.finishedAt)}</span>
                    )}
                  </span>
                </div>
                <span className="spacer" />
                <RunStatusBadge status={run.status} />
              </>
            );
            return workflowPath ? (
              <Link
                className="row-item run-row"
                key={run.id}
                href={runHref(params.project, params.repo, workflowPath, run)}
              >
                {body}
              </Link>
            ) : (
              <div className="row-item run-row" key={run.id}>
                {body}
              </div>
            );
          })
        )}
      </div>

      <div className="panel workflows-panel">
        <h2>
          Workflow definitions
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
              <span className="tree-icon">◇</span>
              <div className="pr-main">
                <Link
                  href={blobHref(params.project, params.repo, defaultBranch, p.workflowPath)}
                  className="nm workflow-src-link"
                  title="View workflow source"
                >
                  {p.name}
                </Link>
                <span className="sub mono">{p.workflowPath}</span>
              </div>
              <span className="spacer" />
              {p.lastRun ? (
                <Link
                  href={runHref(params.project, params.repo, p.workflowPath, p.lastRun)}
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
    </>
  );
}
