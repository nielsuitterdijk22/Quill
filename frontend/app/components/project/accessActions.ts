"use server";

import { revalidatePath } from "next/cache";

import {
  claimProjectOwnership,
  removeProjectGroup,
  removeProjectUser,
  setProjectGroupRole,
  setProjectUserRole,
} from "../../lib/api";
import { getToken } from "../../lib/session";

type ActionResult = { ok: true } | { ok: false; error: string };

async function run(
  project: string,
  fn: (token: string) => Promise<ActionResult>,
): Promise<ActionResult> {
  const token = await getToken();
  if (!token) return { ok: false, error: "Your session has expired. Sign in again." };
  const res = await fn(token);
  if (res.ok) revalidatePath(`/projects/${project}/settings/access`);
  return res;
}

export const setUserRoleAction = (project: string, username: string, role: string) =>
  run(project, (t) => setProjectUserRole(t, project, username.trim(), role));
export const removeUserAction = (project: string, userId: string) =>
  run(project, (t) => removeProjectUser(t, project, userId));
export const setGroupRoleAction = (project: string, group: string, role: string) =>
  run(project, (t) => setProjectGroupRole(t, project, group.trim(), role));
export const removeGroupAction = (project: string, group: string) =>
  run(project, (t) => removeProjectGroup(t, project, group));
export const claimOwnershipAction = (project: string) =>
  run(project, (t) => claimProjectOwnership(t, project));
