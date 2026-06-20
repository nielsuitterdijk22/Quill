import { API_BASE, authGet, postCreate } from "./client";
import type { Project, Repo, ReposResult, MyProject, Result, MutationResult } from "./types";

export async function listProjects(token: string): Promise<Project[]> {
  try {
    const res = await fetch(`${API_BASE}/api/v1/projects`, {
      headers: { Authorization: `Bearer ${token}` },
      cache: "no-store",
    });
    if (!res.ok) return [];
    const data = (await res.json()) as { projects?: Project[] };
    return data.projects ?? [];
  } catch {
    return [];
  }
}

export async function listReposByProject(token: string, slug: string): Promise<Repo[]> {
  try {
    const res = await fetch(`${API_BASE}/api/v1/projects/${slug}/repos`, {
      headers: { Authorization: `Bearer ${token}` },
      cache: "no-store",
    });
    if (!res.ok) return [];
    const data = (await res.json()) as { repositories?: Repo[] };
    return data.repositories ?? [];
  } catch {
    return [];
  }
}

export function getProject(token: string, slug: string): Promise<Result<Project>> {
  return authGet<Project>(token, `/api/v1/projects/${slug}`);
}

export function getReposByProject(token: string, slug: string): Promise<Result<ReposResult>> {
  return authGet<ReposResult>(token, `/api/v1/projects/${slug}/repos`);
}

export function createProject(
  token: string,
  input: { slug: string; name: string; description: string },
): Promise<MutationResult> {
  return postCreate(token, "/api/v1/projects", input);
}

export async function getMyProjects(token: string): Promise<MyProject[]> {
  const res = await authGet<{ projects?: MyProject[] }>(token, "/api/v1/me/projects");
  return res.ok ? (res.data.projects ?? []) : [];
}

export async function getOpenPullRequestCount(token: string): Promise<number> {
  const res = await authGet<{ openPullRequests: number }>(token, "/api/v1/me/pulls/open-count");
  return res.ok ? res.data.openPullRequests : 0;
}
