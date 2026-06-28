import Link from "next/link";
import { notFound } from "next/navigation";

import { getPipelineRun } from "../../../../../../../../lib/api";
import { getToken } from "../../../../../../../../lib/session";
import {
  BrowseError,
  RepoHeader,
  fmtDate,
  repoBase,
  shortSha,
} from "../../../../../../../../components/repo";
import {
  RunStatusBadge,
  StepLogs,
  durationText,
  statusGlyph,
} from "../../../../../../../../components/pipelines";
import { ReRunButton } from "./ReRunButton";
import { CancelButton } from "./CancelButton";
import { LiveLogs } from "./LiveLogs";

function runDuration(startedAt?: string, finishedAt?: string): string {
  return durationText(startedAt, finishedAt) ?? "—";
}

// RunDetailPage shows a single run with its job/step tree and step logs.
export default async function RunDetailPage({
  params,
  searchParams,
}: {
  params: { project: string; repo: string; number: string };
  searchParams: { workflow?: string };
}) {
  const token = await getToken();
  if (!token) notFound();

  const workflow = (searchParams.workflow ?? "").trim();
  const number = Number.parseInt(params.number, 10);
  if (!workflow || !Number.isFinite(number) || number <= 0) notFound();

  const res = await getPipelineRun(token, params.project, params.repo, number, workflow);
  if (!res.ok) {
    if (res.status === 404) notFound();
    return (
      <BrowseError
        project={params.project}
        repo={params.repo}
        status={res.status}
        message={res.message}
      />
    );
  }

  const repo = res.data.repository;
  const run = res.data.run;
  const base = repoBase(params.project, params.repo);
  const workflowPath = run.workflowPath ?? workflow;
  const workflowName = workflowPath.split("/").pop() ?? "Workflow";

  return (
    <>
      <RepoHeader
        project={params.project}
        repo={params.repo}
        visibility={repo.visibility}
        refName={repo.defaultBranch}
        active="pipelines"
      />

      <div className="panel run-hero">
        <div className="run-hero-main">
          <span className={`run-glyph ${run.status}`}>
            {statusGlyph(run.status)}
          </span>
          <div>
            <h1>
              {workflowName}{" "}
              <span className="muted">#{run.runNumber}</span>
            </h1>
            <div className="run-meta">
              <span className="mono">{workflowPath}</span>
              <span>{run.event}</span>
              <span>{run.ref || "—"}</span>
              {run.commitSha && <span className="mono">{shortSha(run.commitSha)}</span>}
              <span>Started {fmtDate(run.createdAt)}</span>
              <span>Duration {runDuration(run.startedAt, run.finishedAt)}</span>
            </div>
          </div>
        </div>
        <div className="run-hero-actions">
          <RunStatusBadge status={run.status} />
          <ReRunButton
            project={params.project}
            repo={params.repo}
            workflowPath={workflowPath}
            gitRef={run.ref || repo.defaultBranch}
          />
          {(run.status === "running" || run.status === "pending") && (
            <CancelButton
              project={params.project}
              repo={params.repo}
              number={run.runNumber}
            />
          )}
          <Link className="btn" href={`${base}/pipelines`}>
            ← All pipelines
          </Link>
        </div>
      </div>

      {/* Active runs: show the live log stream while the pipeline is running */}
      {(run.status === "running" || run.status === "pending") ? (
        <div className="panel">
          <LiveLogs
            project={params.project}
            repo={params.repo}
            runNumber={run.runNumber}
            workflowPath={workflowPath}
          />
        </div>
      ) : run.jobs.length === 0 ? (
        <div className="panel">
          <div className="empty">This run produced no jobs.</div>
        </div>
      ) : (
        run.jobs.map((job) => (
          <div className="panel" key={job.key}>
            <h2>
              <span className={`run-glyph ${job.status}`}>
                {statusGlyph(job.status)}
              </span>{" "}
              {job.name}
              {job.runsOn && <span className="sub mono"> · {job.runsOn}</span>}
              <span className="sub">
                · {runDuration(job.startedAt, job.finishedAt)}
              </span>
              <span className="spacer" />
              <RunStatusBadge status={job.status} />
            </h2>
            {job.steps.map((step, i) => (
              <details className="pipeline-step" key={i} open={step.status === "failure"}>
                <summary>
                  <span className={`run-glyph ${step.status}`}>
                    {statusGlyph(step.status)}
                  </span>
                  <span className="nm">{step.name}</span>
                  <span className="spacer" />
                  <RunStatusBadge status={step.status} />
                </summary>
                <StepLogs logs={step.logs} />
              </details>
            ))}
          </div>
        ))
      )}
    </>
  );
}
