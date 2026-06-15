"use server";

import { createGitToken } from "../lib/api";
import { getToken } from "../lib/session";

export type GitTokenResult =
  | { ok: true; username: string; token: string }
  | { ok: false; error: string };

// generateGitTokenAction mints a one-time git access token for the signed-in
// user. The token is returned to the browser exactly once; Quill never stores or
// re-displays it.
export async function generateGitTokenAction(): Promise<GitTokenResult> {
  const token = getToken();
  if (!token) return { ok: false, error: "Your session has expired. Sign in again." };

  const res = await createGitToken(token);
  if (!res.ok) return { ok: false, error: res.error };

  return { ok: true, username: res.data.username, token: res.data.token };
}
