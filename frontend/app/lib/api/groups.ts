import {
  authGet,
  deleteResource,
  postData,
  sendData,
} from "./client";
import type { DataResult, Result } from "./types";

type SimpleResult = { ok: true } | { ok: false; error: string };

// OrgGroup is a group (department/team) inside an org tenant. Groups nest via
// parentId up to depth 5; members of a subgroup inherit the roles granted to
// its ancestor groups.
export type OrgGroup = {
  id: string;
  slug: string;
  name: string;
  depth: number;
  parentId?: string | null;
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
  parentSlug: string,
): Promise<DataResult<{ group: OrgGroup }>> {
  return postData<{ group: OrgGroup }>(token, `/api/v1/orgs/${org}/groups`, {
    slug,
    name,
    parentSlug,
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
