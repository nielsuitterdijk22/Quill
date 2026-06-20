import { authGet, postData, sendData } from "./client";
import type { Issue, IssueComment, IssuesResult, IssueDetailResult, Result, DataResult } from "./types";

export function listIssues(
  token: string,
  project: string,
  repo: string,
  state: "open" | "closed" | "all" = "open",
  page = 1,
): Promise<Result<IssuesResult>> {
  return authGet<IssuesResult>(
    token,
    `/api/v1/projects/${project}/repos/${repo}/issues?state=${state}&page=${page}`,
  );
}

export function getIssue(
  token: string,
  project: string,
  repo: string,
  number: number,
): Promise<Result<IssueDetailResult>> {
  return authGet<IssueDetailResult>(
    token,
    `/api/v1/projects/${project}/repos/${repo}/issues/${number}`,
  );
}

export function createIssue(
  token: string,
  project: string,
  repo: string,
  input: { title: string; body: string },
): Promise<DataResult<{ issue: Issue }>> {
  return postData(token, `/api/v1/projects/${project}/repos/${repo}/issues`, input);
}

export function editIssue(
  token: string,
  project: string,
  repo: string,
  number: number,
  input: { state?: string; title?: string },
): Promise<DataResult<{ issue: Issue }>> {
  return sendData(
    token,
    "PATCH",
    `/api/v1/projects/${project}/repos/${repo}/issues/${number}`,
    input,
  );
}

export function createIssueComment(
  token: string,
  project: string,
  repo: string,
  number: number,
  body: string,
): Promise<DataResult<{ comment: IssueComment }>> {
  return postData(
    token,
    `/api/v1/projects/${project}/repos/${repo}/issues/${number}/comments`,
    { body },
  );
}
