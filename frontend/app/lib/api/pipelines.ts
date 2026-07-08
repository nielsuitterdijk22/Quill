import { authGet, postData, postNoContent } from "./client";
import type {
  PipelineRun,
  PipelinesResult,
  RunsResult,
  RunDetailResult,
  Result,
  DataResult,
} from "./types";

export function getPipelines(
  token: string,
  project: string,
  repo: string,
): Promise<Result<PipelinesResult>> {
  return authGet<PipelinesResult>(token, `/api/v1/projects/${project}/repos/${repo}/pipelines`);
}

export function getPipelineRuns(
  token: string,
  project: string,
  repo: string,
): Promise<Result<RunsResult>> {
  return authGet<RunsResult>(token, `/api/v1/projects/${project}/repos/${repo}/pipelines/runs`);
}

export function getPipelineRun(
  token: string,
  project: string,
  repo: string,
  number: number,
  workflow: string,
): Promise<Result<RunDetailResult>> {
  return authGet<RunDetailResult>(
    token,
    `/api/v1/projects/${project}/repos/${repo}/pipelines/runs/${number}?workflow=${encodeURIComponent(workflow)}`,
  );
}

export function triggerPipelineRun(
  token: string,
  project: string,
  repo: string,
  input: { workflow: string; ref?: string; environment?: string },
): Promise<DataResult<{ run: PipelineRun }>> {
  return postData(token, `/api/v1/projects/${project}/repos/${repo}/pipelines`, input);
}

export function cancelPipelineRun(
  token: string,
  project: string,
  repo: string,
  number: number,
): Promise<{ ok: true } | { ok: false; error: string }> {
  return postNoContent(
    token,
    `/api/v1/projects/${project}/repos/${repo}/pipelines/runs/${number}/cancel`,
    {},
  );
}
