"use server";

import { revalidatePath } from "next/cache";

import { editIssue } from "../../../../../../../lib/api";
import { getToken } from "../../../../../../../lib/session";
import type { EditTextState } from "../../../../../../../components/EditableDescription";

// editIssueAction saves an issue's edited title and description.
export async function editIssueAction(
  project: string,
  repo: string,
  number: number,
  _prev: EditTextState,
  formData: FormData,
): Promise<EditTextState> {
  const token = await getToken();
  if (!token) return { error: "Your session has expired. Sign in again." };
  const title = String(formData.get("title") ?? "").trim();
  if (!title) return { error: "A title is required." };
  const body = String(formData.get("body") ?? "");
  const res = await editIssue(token, project, repo, number, { title, body });
  if (!res.ok) return { error: res.error };
  revalidatePath(`/projects/${project}/repos/${repo}/issues/${number}`);
  return { ok: true, at: Date.now() };
}
