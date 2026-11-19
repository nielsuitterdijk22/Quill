import { authGet, deleteResource, postData, sendData } from "./client";
import type { Result } from "./types";

type SimpleResult = { ok: true } | { ok: false; error: string };

export type ProjectAccessUser = {
  userId: string;
  username: string;
  displayName: string;
  role: "owner" | "member" | "read";
  since: string;
};

export type ProjectAccessGroup = {
  groupId: string;
  slug: string;
  name: string;
  role: "owner" | "member" | "read";
  since: string;
};

export type ProjectAccess = {
  users: ProjectAccessUser[];
  groups: ProjectAccessGroup[];
};

const base = (project: string) => `/api/v1/projects/${encodeURIComponent(project)}/access`;
const done = (r: { ok: boolean; error?: string }): SimpleResult =>
  r.ok ? { ok: true } : { ok: false, error: r.error ?? "Request failed." };

export function getProjectAccess(token: string, project: string): Promise<Result<ProjectAccess>> {
  return authGet<ProjectAccess>(token, base(project));
}

export function setProjectUserRole(token: string, project: string, username: string, role: string): Promise<SimpleResult> {
  return sendData<{ ok: true }>(token, "PUT", `${base(project)}/users/${encodeURIComponent(username)}`, { role }).then(done);
}

export function removeProjectUser(token: string, project: string, userId: string): Promise<SimpleResult> {
  return deleteResource(token, `${base(project)}/users/${userId}`);
}

export function setProjectGroupRole(token: string, project: string, group: string, role: string): Promise<SimpleResult> {
  return sendData<{ ok: true }>(token, "PUT", `${base(project)}/groups/${encodeURIComponent(group)}`, { role }).then(done);
}

export function removeProjectGroup(token: string, project: string, group: string): Promise<SimpleResult> {
  return deleteResource(token, `${base(project)}/groups/${encodeURIComponent(group)}`);
}

// claimProjectOwnership is the org admin's audited break-glass.
export function claimProjectOwnership(token: string, project: string): Promise<SimpleResult> {
  return postData<{ ok: true }>(token, `${base(project)}/claim`, {}).then(done);
}
