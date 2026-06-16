import Link from "next/link";
import { notFound } from "next/navigation";

import { getPipelineRun } from "../../../../../../../../lib/api";
import { getToken } from "../../../../../../../../lib/session";
import {
  BrowseError,
  RepoHeader,
  fmtDate,
  repoBase,
} from "../../../../../../../../components/repo";
import {
  RunStatusBadge,
  StepLogs,
  statusGlyph,
} from "../../../../../../../../components/pipelines";

// RunDetailPage shows a single run with its job/step tree and step logs.
export default async function RunDetailPage({
  params,
  searchParams,
}: {
  params: { org: string; repo: string; number: string };
  searchParams: { workflow?: string };
}) {
  const token = getToken();
  if (!token) notFound();

  const workflow = (searchParams.workflow ?? "").trim();
  const number = Number.parseInt(params.number, 10);
  if (!workflow || !Number.isFinite(number) || number <= 0) notFound();

  const res = await getPipelineRun(token, params.org, params.repo, number, workflow);
  if (!res.ok) {
    if (res.status === 404) notFound();
    return (
      <BrowseError
        org={params.org}
        repo={params.repo}
        status={res.status}
        message={res.message}
      />
    );
  }

  const repo = res.data.repository;
  const run = res.data.run;
  const base = repoBase(params.org, params.repo);

  return (
    <>
      <RepoHeader
        org={params.org}
        repo={params.repo}
        visibility={repo.visibility}
        refName={repo.defaultBranch}
        active="pipelines"
      />

      <div className="top">
        <h1>
          Run <b>#{run.runNumber}</b>
        </h1>
        <RunStatusBadge status={run.status} />
      </div>

      <div className="panel">
        <div className="run-meta">
          <span className="sub mono">{run.workflowPath}</span>
          <span className="sub">Event: {run.event}</span>
          <span className="sub">Ref: {run.ref || "—"}</span>
          {run.commitSha && (
            <span className="sub mono">{run.commitSha.slice(0, 10)}</span>
          )}
          <span className="sub">Started {fmtDate(run.createdAt)}</span>
        </div>
        <Link className="btn" href={`${base}/pipelines`}>
          ← All pipelines
        </Link>
      </div>

      {run.jobs.length === 0 ? (
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
