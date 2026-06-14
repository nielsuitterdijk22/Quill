"use server";

import { revalidatePath } from "next/cache";

import { createPullComment, mergePull } from "../../../../../../lib/api";
import { getToken } from "../../../../../../lib/session";

export type CommentState = { error?: string; ok?: boolean };

// addCommentAction posts a conversation comment and refreshes the PR page.
export async function addCommentAction(
  org: string,
  repo: string,
  number: number,
  _prev: CommentState,
  formData: FormData,
): Promise<CommentState> {
  const token = getToken();
  if (!token) return { error: "Your session has expired. Sign in again." };

  const body = String(formData.get("body") ?? "").trim();
  if (!body) return { error: "Write a comment first." };

  const res = await createPullComment(token, org, repo, number, body);
  if (!res.ok) return { error: res.error };

  revalidatePath(`/orgs/${org}/${repo}/pulls/${number}`);
  return { ok: true };
}

export type MergeState = { error?: string };

// mergeAction merges the pull request and refreshes the page.
export async function mergeAction(
  org: string,
  repo: string,
  number: number,
  _prev: MergeState,
  formData: FormData,
): Promise<MergeState> {
  const token = getToken();
  if (!token) return { error: "Your session has expired. Sign in again." };

  const method = String(formData.get("method") ?? "merge");
  const allowed = method === "squash" || method === "rebase" ? method : "merge";

  const res = await mergePull(token, org, repo, number, allowed);
  if (!res.ok) return { error: res.error };

  revalidatePath(`/orgs/${org}/${repo}/pulls/${number}`);
  return {};
}
