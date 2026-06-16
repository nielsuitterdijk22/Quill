"use server";

import { revalidatePath } from "next/cache";

import { createGitToken, revokeGitToken } from "../lib/api";
import { getToken } from "../lib/session";

export type GitTokenResult =
  | { ok: true; id: string; username: string; token: string }
  | { ok: false; error: string };

// generateGitTokenAction mints a one-time git access token for the signed-in
// user. The token is returned to the browser exactly once; Quill never stores or
// re-displays the secret. It does record the token's metadata so it can be listed
// and revoked, so the settings page is refreshed afterwards.
export async function generateGitTokenAction(name: string): Promise<GitTokenResult> {
  const token = getToken();
  if (!token) return { ok: false, error: "Your session has expired. Sign in again." };

  const res = await createGitToken(token, name);
  if (!res.ok) return { ok: false, error: res.error };

  revalidatePath("/settings");
  return { ok: true, id: res.data.id, username: res.data.username, token: res.data.token };
}

export type RevokeTokenResult = { ok: true } | { ok: false; error: string };

// revokeGitTokenAction revokes one of the signed-in user's git tokens, then
// refreshes the settings page so the list reflects the change.
export async function revokeGitTokenAction(id: string): Promise<RevokeTokenResult> {
  const token = getToken();
  if (!token) return { ok: false, error: "Your session has expired. Sign in again." };

  const res = await revokeGitToken(token, id);
  if (!res.ok) return res;

  revalidatePath("/settings");
  return { ok: true };
}
