import Link from "next/link";
import { notFound } from "next/navigation";

import { getPipelineRun } from "../../../../../../../../lib/api";
import { getToken } from "../../../../../../../../lib/session";
import { BrowseError, RepoHeader, repoBase } from "../../../../../../../../components/repo";
import { ReRunButton } from "./ReRunButton";
import { CancelButton } from "./CancelButton";
import { RunView } from "./RunView";

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

  return (
    <>
      <RepoHeader
        project={params.project}
        repo={params.repo}
        visibility={repo.visibility}
        refName={repo.defaultBranch}
        active="pipelines"
      />
      <RunView
        project={params.project}
        repo={params.repo}
        workflowPath={workflowPath}
        initial={run}
        actions={
          <>
            <ReRunButton
              project={params.project}
              repo={params.repo}
              workflowPath={workflowPath}
              gitRef={run.ref || repo.defaultBranch}
            />
            {(run.status === "running" || run.status === "pending") && (
              <CancelButton project={params.project} repo={params.repo} number={run.runNumber} />
            )}
            <Link className="btn" href={`${base}/pipelines`}>
              ← All pipelines
            </Link>
          </>
        }
      />
    </>
  );
}
