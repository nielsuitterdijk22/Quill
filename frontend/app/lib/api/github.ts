import { authGet, postData } from "./client";

export type GitHubRepo = {
  id: number;
  name: string;
  fullName: string;
  description: string;
  private: boolean;
  cloneUrl: string;
  htmlUrl: string;
};

export type ImportResult = {
  name: string;
  ok: boolean;
  error?: string;
};

export async function listGitHubRepos(token: string): Promise<GitHubRepo[]> {
  const res = await authGet<{ repos: GitHubRepo[] }>(token, "/api/v1/import/github/repos");
  return res.ok ? res.data.repos : [];
}

export function importGitHubRepos(
  token: string,
  projectSlug: string,
  repos: { name: string; cloneUrl: string; description: string; private: boolean }[],
) {
  return postData<{ results: ImportResult[] }>(token, "/api/v1/import/github", {
    projectSlug,
    repos,
  });
}
