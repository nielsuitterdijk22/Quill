import { authGet, postData, sendData, postCreate, putResource, deleteResource } from "./client";
import type {
  Repo,
  UpdateRepoInput,
  BranchesResult,
  CommitsResult,
  CommitDetailResult,
  ContentsResult,
  Result,
  DataResult,
  MutationResult,
} from "./types";

export function getRepo(token: string, project: string, repo: string): Promise<Result<Repo>> {
  return authGet<Repo>(token, `/api/v1/projects/${project}/repos/${repo}`);
}

export function createRepo(
  token: string,
  project: string,
  input: { slug: string; name: string; description: string; visibility: string },
): Promise<MutationResult> {
  return postCreate(token, `/api/v1/projects/${project}/repos`, input);
}

export function updateRepo(
  token: string,
  project: string,
  repo: string,
  input: UpdateRepoInput,
): Promise<DataResult<Repo>> {
  return sendData<Repo>(token, "PATCH", `/api/v1/projects/${project}/repos/${repo}`, input);
}

export function deleteRepo(
  token: string,
  project: string,
  repo: string,
): Promise<{ ok: true } | { ok: false; error: string }> {
  return deleteResource(token, `/api/v1/projects/${project}/repos/${repo}`);
}

export function forkRepo(
  token: string,
  project: string,
  repo: string,
  input: { targetProject: string; slug: string },
): Promise<DataResult<{ repository: Repo }>> {
  return postData(token, `/api/v1/projects/${project}/repos/${repo}/fork`, input);
}

export function starRepo(
  token: string,
  project: string,
  repo: string,
): Promise<{ ok: true } | { ok: false; error: string }> {
  return putResource(token, `/api/v1/projects/${project}/repos/${repo}/star`);
}

export function unstarRepo(
  token: string,
  project: string,
  repo: string,
): Promise<{ ok: true } | { ok: false; error: string }> {
  return deleteResource(token, `/api/v1/projects/${project}/repos/${repo}/star`);
}

export function getBranches(
  token: string,
  project: string,
  repo: string,
): Promise<Result<BranchesResult>> {
  return authGet<BranchesResult>(token, `/api/v1/projects/${project}/repos/${repo}/branches`);
}

export function getCommits(
  token: string,
  project: string,
  repo: string,
  ref?: string,
  path?: string,
  limit = 30,
): Promise<Result<CommitsResult>> {
  const q = new URLSearchParams();
  if (ref) q.set("ref", ref);
  if (path) q.set("path", path);
  q.set("limit", String(limit));
  return authGet<CommitsResult>(token, `/api/v1/projects/${project}/repos/${repo}/commits?${q.toString()}`);
}

export function getCommit(
  token: string,
  project: string,
  repo: string,
  sha: string,
): Promise<Result<CommitDetailResult>> {
  return authGet<CommitDetailResult>(token, `/api/v1/projects/${project}/repos/${repo}/commits/${sha}`);
}

export function getContents(
  token: string,
  project: string,
  repo: string,
  path?: string,
  ref?: string,
): Promise<Result<ContentsResult>> {
  const q = new URLSearchParams();
  if (path) q.set("path", path);
  if (ref) q.set("ref", ref);
  const qs = q.toString();
  return authGet<ContentsResult>(
    token,
    `/api/v1/projects/${project}/repos/${repo}/contents${qs ? `?${qs}` : ""}`,
  );
}

export async function renderMarkdown(
  token: string,
  project: string,
  repo: string,
  text: string,
): Promise<string | null> {
  const res = await postData<{ html: string }>(
    token,
    `/api/v1/projects/${project}/repos/${repo}/markup`,
    { text },
  );
  return res.ok ? res.data.html : null;
}
