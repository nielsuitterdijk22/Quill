"use server";

import { redirect } from "next/navigation";

import { createPull } from "../../../../../../../lib/api";
import { getToken } from "../../../../../../../lib/session";

export type CreatePullState = { error?: string };

// createPullAction opens a pull request, then redirects to it. The project and repo
// slugs are bound from the route params.
export async function createPullAction(
  project: string,
  repo: string,
  _prev: CreatePullState,
  formData: FormData,
): Promise<CreatePullState> {
  const token = await getToken();
  if (!token) return { error: "Your session has expired. Sign in again." };

  const title = String(formData.get("title") ?? "").trim();
  const body = String(formData.get("body") ?? "").trim();
  const head = String(formData.get("head") ?? "").trim();
  const base = String(formData.get("base") ?? "").trim();

  if (!title) return { error: "Enter a title for the pull request." };
  if (!head || !base) return { error: "Choose a source and target branch." };
  if (head === base) {
    return { error: "The source and target branch must differ." };
  }

  const result = await createPull(token, project, repo, {
    title,
    body,
    head,
    base,
  });
  if (!result.ok) return { error: result.error };

  redirect(`/projects/${project}/repos/${repo}/pulls/${result.data.pull.number}`);
}
