"use server";

import { revalidatePath } from "next/cache";

import { addTeamMember, removeTeamMember } from "../../../../../lib/api";
import { getToken } from "../../../../../lib/session";

export type MemberFormState = { error?: string; ok?: boolean };

const ROLES = new Set(["maintainer", "member"]);

// addTeamMemberAction adds (or updates the role of) a user in a team by username,
// then refreshes the team page. The org/team slugs are bound from route params.
export async function addTeamMemberAction(
  org: string,
  team: string,
  _prev: MemberFormState,
  formData: FormData,
): Promise<MemberFormState> {
  const token = getToken();
  if (!token) return { error: "Your session has expired. Sign in again." };

  const username = String(formData.get("username") ?? "").trim();
  if (!username) return { error: "Enter a username." };

  const role = String(formData.get("role") ?? "member").trim();
  if (!ROLES.has(role)) return { error: "Choose a valid role." };

  const res = await addTeamMember(token, org, team, { username, role });
  if (!res.ok) return { error: res.error };

  revalidatePath(`/orgs/${org}/teams/${team}`);
  return { ok: true };
}

// removeTeamMemberAction removes a user from a team by id, then refreshes. The
// org/team slugs and user id are bound from the route / row.
export async function removeTeamMemberAction(
  org: string,
  team: string,
  userID: string,
  _prev: MemberFormState,
  _formData: FormData,
): Promise<MemberFormState> {
  const token = getToken();
  if (!token) return { error: "Your session has expired. Sign in again." };

  const res = await removeTeamMember(token, org, team, userID);
  if (!res.ok) return { error: res.error };

  revalidatePath(`/orgs/${org}/teams/${team}`);
  return { ok: true };
}
