"use server";

import { revalidatePath } from "next/cache";

import {
  createOrgInvite,
  removeOrgMember,
  revokeOrgInvite,
  setOrgMemberRole,
} from "../../lib/api";
import { getToken } from "../../lib/session";

// Server actions backing the org Members surface. Each resolves the session
// token, performs the mutation, and revalidates the org settings page so the
// refreshed roster/invite list re-renders. inviteMemberAction additionally
// returns the one-time accept token so the client can show a shareable link.

type ActionResult = { ok: true } | { ok: false; error: string };

function revalidate(org: string) {
  revalidatePath(`/orgs/${org}/settings`);
}

export async function inviteMemberAction(
  org: string,
  email: string,
  role: string,
): Promise<{ ok: true; token: string; emailedByIdp: boolean } | { ok: false; error: string }> {
  const token = await getToken();
  if (!token) return { ok: false, error: "Your session has expired. Sign in again." };
  const res = await createOrgInvite(token, org, email.trim(), role);
  if (!res.ok) return { ok: false, error: res.error };
  revalidate(org);
  return { ok: true, token: res.data.token, emailedByIdp: res.data.emailedByIdp };
}

export async function revokeInviteAction(org: string, id: string): Promise<ActionResult> {
  const token = await getToken();
  if (!token) return { ok: false, error: "Your session has expired. Sign in again." };
  const res = await revokeOrgInvite(token, org, id);
  if (!res.ok) return res;
  revalidate(org);
  return { ok: true };
}

export async function setMemberRoleAction(
  org: string,
  userId: string,
  role: string,
): Promise<ActionResult> {
  const token = await getToken();
  if (!token) return { ok: false, error: "Your session has expired. Sign in again." };
  const res = await setOrgMemberRole(token, org, userId, role);
  if (!res.ok) return { ok: false, error: res.error };
  revalidate(org);
  return { ok: true };
}

export async function removeMemberAction(org: string, userId: string): Promise<ActionResult> {
  const token = await getToken();
  if (!token) return { ok: false, error: "Your session has expired. Sign in again." };
  const res = await removeOrgMember(token, org, userId);
  if (!res.ok) return res;
  revalidate(org);
  return { ok: true };
}
