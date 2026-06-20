"use server";

import { revalidatePath } from "next/cache";

import {
  createLineComment,
  createPullComment,
  createPullReview,
  mergePull,
  type ReviewState,
} from "../../../../../../../lib/api";
import { getToken } from "../../../../../../../lib/session";

export type CommentState = { error?: string; ok?: boolean };

// addCommentAction posts a conversation comment and refreshes the PR page.
export async function addCommentAction(
  project: string,
  repo: string,
  number: number,
  _prev: CommentState,
  formData: FormData,
): Promise<CommentState> {
  const token = await getToken();
  if (!token) return { error: "Your session has expired. Sign in again." };

  const body = String(formData.get("body") ?? "").trim();
  if (!body) return { error: "Write a comment first." };

  const res = await createPullComment(token, project, repo, number, body);
  if (!res.ok) return { error: res.error };

  revalidatePath(`/projects/${project}/repos/${repo}/pulls/${number}`);
  return { ok: true };
}

export type LineCommentState = { error?: string; ok?: boolean };

// addLineCommentAction posts a single line-anchored review comment on the PR's
// diff (line is the new-file line number) and refreshes the Files tab.
export async function addLineCommentAction(
  project: string,
  repo: string,
  number: number,
  path: string,
  line: number,
  _prev: LineCommentState,
  formData: FormData,
): Promise<LineCommentState> {
  const token = await getToken();
  if (!token) return { error: "Your session has expired. Sign in again." };

  const body = String(formData.get("body") ?? "").trim();
  if (!body) return { error: "Write a comment first." };

  const res = await createLineComment(token, project, repo, number, {
    path,
    line,
    body,
  });
  if (!res.ok) return { error: res.error };

  revalidatePath(`/projects/${project}/repos/${repo}/pulls/${number}`);
  return { ok: true };
}

export type MergeState = { error?: string; ok?: boolean };

// mergeAction merges the pull request and refreshes the page.
export async function mergeAction(
  project: string,
  repo: string,
  number: number,
  _prev: MergeState,
  formData: FormData,
): Promise<MergeState> {
  const token = await getToken();
  if (!token) return { error: "Your session has expired. Sign in again." };

  const method = String(formData.get("method") ?? "merge");
  const allowed = method === "squash" || method === "rebase" ? method : "merge";

  const res = await mergePull(token, project, repo, number, allowed);
  if (!res.ok) return { error: res.error };

  revalidatePath(`/projects/${project}/repos/${repo}/pulls/${number}`);
  return {};
}

export type ReviewActionState = { error?: string; ok?: boolean };

// reviewAction submits a review (approve, request changes, or comment) and
// refreshes the PR so the gate and conversation reflect it.
export async function reviewAction(
  project: string,
  repo: string,
  number: number,
  _prev: ReviewActionState,
  formData: FormData,
): Promise<ReviewActionState> {
  const token = await getToken();
  if (!token) return { error: "Your session has expired. Sign in again." };

  const event = String(formData.get("event") ?? "");
  if (
    event !== "APPROVED" &&
    event !== "REQUEST_CHANGES" &&
    event !== "COMMENT"
  ) {
    return { error: "Choose a review action." };
  }
  const body = String(formData.get("body") ?? "").trim();
  if (event === "COMMENT" && !body) {
    return { error: "Write a comment for a comment-only review." };
  }

  const res = await createPullReview(token, project, repo, number, {
    event: event as ReviewState,
    body,
  });
  if (!res.ok) return { error: res.error };

  revalidatePath(`/projects/${project}/repos/${repo}/pulls/${number}`);
  return { ok: true };
}
