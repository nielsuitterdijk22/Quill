import { authGet, postData } from "./client";
import type {
  PullRequest,
  PullComment,
  Review,
  ReviewState,
  LineComment,
  PullsResult,
  MyPullsResult,
  PullResult,
  DiffResult,
  CommentsResult,
  ReviewsResult,
  Result,
  DataResult,
  Commit,
} from "./types";

export function getPulls(
  token: string,
  project: string,
  repo: string,
  state: "open" | "closed" | "all" = "open",
): Promise<Result<PullsResult>> {
  return authGet<PullsResult>(token, `/api/v1/projects/${project}/repos/${repo}/pulls?state=${state}`);
}

export function getMyPulls(
  token: string,
  opts: { state?: "open" | "closed" | "all"; project?: string } = {},
): Promise<Result<MyPullsResult>> {
  const q = new URLSearchParams();
  if (opts.state) q.set("state", opts.state);
  if (opts.project) q.set("project", opts.project);
  const suffix = q.toString() ? `?${q.toString()}` : "";
  return authGet<MyPullsResult>(token, `/api/v1/me/pulls${suffix}`);
}

export function getPull(
  token: string,
  project: string,
  repo: string,
  number: number,
): Promise<Result<PullResult>> {
  return authGet<PullResult>(token, `/api/v1/projects/${project}/repos/${repo}/pulls/${number}`);
}

export function getPullDiff(
  token: string,
  project: string,
  repo: string,
  number: number,
): Promise<Result<DiffResult>> {
  return authGet<DiffResult>(token, `/api/v1/projects/${project}/repos/${repo}/pulls/${number}/diff`);
}

export function getPullComments(
  token: string,
  project: string,
  repo: string,
  number: number,
): Promise<Result<CommentsResult>> {
  return authGet<CommentsResult>(
    token,
    `/api/v1/projects/${project}/repos/${repo}/pulls/${number}/comments`,
  );
}

export function getPullReviews(
  token: string,
  project: string,
  repo: string,
  number: number,
): Promise<Result<ReviewsResult>> {
  return authGet<ReviewsResult>(
    token,
    `/api/v1/projects/${project}/repos/${repo}/pulls/${number}/reviews`,
  );
}

export function getPullCommits(
  token: string,
  project: string,
  repo: string,
  number: number,
): Promise<Result<{ commits: Commit[] }>> {
  return authGet<{ commits: Commit[] }>(
    token,
    `/api/v1/projects/${project}/repos/${repo}/pulls/${number}/commits`,
  );
}

export function getLineComments(
  token: string,
  project: string,
  repo: string,
  number: number,
): Promise<Result<{ comments: LineComment[] }>> {
  return authGet<{ comments: LineComment[] }>(
    token,
    `/api/v1/projects/${project}/repos/${repo}/pulls/${number}/line-comments`,
  );
}

export function createPull(
  token: string,
  project: string,
  repo: string,
  input: { title: string; body: string; head: string; base: string },
): Promise<DataResult<{ pull: PullRequest }>> {
  return postData(token, `/api/v1/projects/${project}/repos/${repo}/pulls`, input);
}

export function createPullComment(
  token: string,
  project: string,
  repo: string,
  number: number,
  body: string,
): Promise<DataResult<{ comment: PullComment }>> {
  return postData(
    token,
    `/api/v1/projects/${project}/repos/${repo}/pulls/${number}/comments`,
    { body },
  );
}

export function mergePull(
  token: string,
  project: string,
  repo: string,
  number: number,
  method: "merge" | "squash" | "rebase",
): Promise<DataResult<{ pull: PullRequest }>> {
  return postData(
    token,
    `/api/v1/projects/${project}/repos/${repo}/pulls/${number}/merge`,
    { method },
  );
}

export function createPullReview(
  token: string,
  project: string,
  repo: string,
  number: number,
  input: { event: ReviewState; body: string },
): Promise<DataResult<{ review: Review }>> {
  return postData(
    token,
    `/api/v1/projects/${project}/repos/${repo}/pulls/${number}/reviews`,
    input,
  );
}

export function createLineComment(
  token: string,
  project: string,
  repo: string,
  number: number,
  input: { path: string; line: number; body: string },
): Promise<DataResult<{ comment: LineComment }>> {
  return postData(
    token,
    `/api/v1/projects/${project}/repos/${repo}/pulls/${number}/line-comments`,
    input,
  );
}
