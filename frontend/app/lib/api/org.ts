import { authGet, deleteResource, postNoContent } from "./client";
import type { Result, OrgMembersResult, OrgInvitationsResult } from "./types";

export function listOrgMembers(token: string): Promise<Result<OrgMembersResult>> {
  return authGet<OrgMembersResult>(token, "/api/v1/org/members");
}

export function listOrgInvitations(token: string): Promise<Result<OrgInvitationsResult>> {
  return authGet<OrgInvitationsResult>(token, "/api/v1/org/invitations");
}

export function inviteOrgMember(
  token: string,
  email: string,
  role: string,
): Promise<{ ok: true } | { ok: false; error: string }> {
  return postNoContent(token, "/api/v1/org/members", { email, role });
}

export function removeOrgMember(
  token: string,
  membershipId: string,
): Promise<{ ok: true } | { ok: false; error: string }> {
  return deleteResource(token, `/api/v1/org/members/${membershipId}`);
}

export function revokeOrgInvitation(
  token: string,
  invitationId: string,
): Promise<{ ok: true } | { ok: false; error: string }> {
  return deleteResource(token, `/api/v1/org/invitations/${invitationId}`);
}
