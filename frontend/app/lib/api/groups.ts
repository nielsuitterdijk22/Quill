import {
  authGet,
  deleteResource,
  postData,
  sendData,
} from "./client";
import type { DataResult, Result } from "./types";

type SimpleResult = { ok: true } | { ok: false; error: string };

// OrgGroup is a named set of people inside an org tenant. Groups are flat: a
// project grants a role to the group, and exactly its members receive it.
export type OrgGroup = {
  id: string;
  slug: string;
  name: string;
  // "scim" groups belong to the identity provider: roster is read-only.
  source?: "manual" | "scim";
};

export type GroupMember = {
  userId: string;
  username: string;
  email: string;
  displayName: string;
  since: string;
};

export function getOrgGroups(
  token: string,
  org: string,
): Promise<Result<{ groups: OrgGroup[] }>> {
  return authGet<{ groups: OrgGroup[] }>(token, `/api/v1/orgs/${org}/groups`);
}

export function createOrgGroup(
  token: string,
  org: string,
  slug: string,
  name: string,
): Promise<DataResult<{ group: OrgGroup }>> {
  return postData<{ group: OrgGroup }>(token, `/api/v1/orgs/${org}/groups`, {
    slug,
    name,
  });
}

export function deleteOrgGroup(
  token: string,
  org: string,
  group: string,
): Promise<SimpleResult> {
  return deleteResource(token, `/api/v1/orgs/${org}/groups/${group}`);
}

export function getGroupMembers(
  token: string,
  org: string,
  group: string,
): Promise<Result<{ members: GroupMember[] }>> {
  return authGet<{ members: GroupMember[] }>(
    token,
    `/api/v1/orgs/${org}/groups/${group}/members`,
  );
}

export function addGroupMember(
  token: string,
  org: string,
  group: string,
  userId: string,
): Promise<SimpleResult> {
  return postData<{ ok: true }>(token, `/api/v1/orgs/${org}/groups/${group}/members`, {
    userId,
  }).then((r) => (r.ok ? { ok: true } : { ok: false, error: r.error }));
}

export function removeGroupMember(
  token: string,
  org: string,
  group: string,
  userId: string,
): Promise<SimpleResult> {
  return deleteResource(
    token,
    `/api/v1/orgs/${org}/groups/${group}/members/${userId}`,
  );
}

export function setGroupProjectRole(
  token: string,
  org: string,
  group: string,
  project: string,
  role: string,
): Promise<SimpleResult> {
  return sendData<{ ok: true }>(
    token,
    "PUT",
    `/api/v1/orgs/${org}/groups/${group}/projects/${project}`,
    { role },
  ).then((r) => (r.ok ? { ok: true } : { ok: false, error: r.error }));
}

export function removeGroupProjectRole(
  token: string,
  org: string,
  group: string,
  project: string,
): Promise<SimpleResult> {
  return deleteResource(
    token,
    `/api/v1/orgs/${org}/groups/${group}/projects/${project}`,
  );
}

// GroupProjectAccess is one project a group is granted a role on.
export type GroupProjectAccess = { project: string; role: string; since: string };

export function getGroupProjects(
  token: string,
  org: string,
  group: string,
): Promise<Result<{ projects: GroupProjectAccess[] }>> {
  return authGet<{ projects: GroupProjectAccess[] }>(token, `/api/v1/orgs/${org}/groups/${group}/projects`);
}

// OrgProjectRef is a project of an organization (for pickers).
export type OrgProjectRef = { slug: string; name: string };

export function getOrgProjects(token: string, org: string): Promise<Result<{ projects: OrgProjectRef[] }>> {
  return authGet<{ projects: OrgProjectRef[] }>(token, `/api/v1/orgs/${org}/projects`);
}
