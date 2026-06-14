"use server";

import { revalidatePath } from "next/cache";

import {
  createPullComment,
  createPullReview,
  mergePull,
  type ReviewState,
} from "../../../../../../lib/api";
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

export type ReviewActionState = { error?: string; ok?: boolean };

// reviewAction submits a review (approve, request changes, or comment) and
// refreshes the PR so the gate and conversation reflect it.
export async function reviewAction(
  org: string,
  repo: string,
  number: number,
  _prev: ReviewActionState,
  formData: FormData,
): Promise<ReviewActionState> {
  const token = getToken();
  if (!token) return { error: "Your session has expired. Sign in again." };

  const event = String(formData.get("event") ?? "");
  if (event !== "APPROVED" && event !== "REQUEST_CHANGES" && event !== "COMMENT") {
    return { error: "Choose a review action." };
  }
  const body = String(formData.get("body") ?? "").trim();
  if (event === "COMMENT" && !body) {
    return { error: "Write a comment for a comment-only review." };
  }

  const res = await createPullReview(token, org, repo, number, {
    event: event as ReviewState,
    body,
  });
  if (!res.ok) return { error: res.error };

  revalidatePath(`/orgs/${org}/${repo}/pulls/${number}`);
  return { ok: true };
}
