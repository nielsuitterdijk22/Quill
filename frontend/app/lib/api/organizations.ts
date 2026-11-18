import {
  API_BASE,
  authGet,
  deleteResource,
  postData,
  sendData,
} from "./client";
import type { DataResult, Result } from "./types";

// SimpleResult mirrors deleteResource's return shape for mutations that only
// signal success/failure (no payload).
type SimpleResult = { ok: true } | { ok: false; error: string };

// Organization is an org-kind tenant the signed-in user belongs to, with their
// role in it. Distinct from a Project: an org owns projects and adds member, SSO,
// and org-wide policy management.
export type Organization = {
  slug: string;
  name: string;
  role: string; // 'admin' | 'member'
  plan?: string; // 'inactive' | 'team' | 'lapsed' | 'enterprise' | 'internal' | 'beta' | …
  locked?: boolean; // no subscription yet / subscription ended: new work paused
};

export type OrgMember = {
  userId: string;
  username: string;
  email: string;
  displayName: string;
  role: string; // 'admin' | 'member'
  since: string;
};

export type OrgInvite = {
  id: string;
  email: string;
  role: string;
  expiresAt: string;
  createdAt: string;
};

// CreateInviteResult carries the one-time accept token so the caller can build a
// shareable link, plus whether the IdP was asked to email it.
export type CreateInviteResult = {
  invite: OrgInvite;
  token: string;
  emailedByIdp: boolean;
};

export function getOrgMembers(
  token: string,
  org: string,
): Promise<Result<{ members: OrgMember[] }>> {
  return authGet<{ members: OrgMember[] }>(token, `/api/v1/orgs/${org}/members`);
}

export function getOrgInvites(
  token: string,
  org: string,
): Promise<Result<{ invites: OrgInvite[] }>> {
  return authGet<{ invites: OrgInvite[] }>(token, `/api/v1/orgs/${org}/invites`);
}

export function createOrgInvite(
  token: string,
  org: string,
  email: string,
  role: string,
): Promise<DataResult<CreateInviteResult>> {
  return postData<CreateInviteResult>(token, `/api/v1/orgs/${org}/invites`, { email, role });
}

export function revokeOrgInvite(
  token: string,
  org: string,
  id: string,
): Promise<SimpleResult> {
  return deleteResource(token, `/api/v1/orgs/${org}/invites/${id}`);
}

export function setOrgMemberRole(
  token: string,
  org: string,
  userId: string,
  role: string,
): Promise<DataResult<{ ok: boolean }>> {
  return sendData<{ ok: boolean }>(token, "PATCH", `/api/v1/orgs/${org}/members/${userId}`, { role });
}

export function removeOrgMember(
  token: string,
  org: string,
  userId: string,
): Promise<SimpleResult> {
  return deleteResource(token, `/api/v1/orgs/${org}/members/${userId}`);
}

export function acceptInvite(
  token: string,
  inviteToken: string,
): Promise<DataResult<{ slug: string }>> {
  return postData<{ slug: string }>(token, `/api/v1/invites/${inviteToken}/accept`, {});
}

// SSOConfig is an org's SSO configuration. The client secret is never returned —
// hasSecret only reports whether one is stored.
export type SSOConfig = {
  configured: boolean;
  protocol: string; // 'oidc' | 'saml'
  issuer: string;
  clientId: string;
  emailDomain: string;
  enabled: boolean;
  hasSecret: boolean;
  updatedAt: string;
  domainVerified?: boolean;
  verifyRecordName?: string;
  verifyRecordValue?: string;
  active?: boolean; // the IdP is live
  routing?: boolean; // the email domain sends sign-ins to it
  redirectUri?: string; // OIDC: register at the IdP
  samlEntityId?: string;
  samlAcsUrl?: string;
  identityOrgId?: string;
  lastError?: string;
  available?: boolean;
  planAllowsSso?: boolean;
};

export type SetSSOInput = {
  protocol: string;
  issuer: string;
  clientId: string;
  clientSecret: string; // blank preserves the stored secret
  emailDomain: string;
  enabled: boolean;
};

export function getOrgSSO(token: string, org: string): Promise<Result<SSOConfig>> {
  return authGet<SSOConfig>(token, `/api/v1/orgs/${org}/sso`);
}

export function setOrgSSO(
  token: string,
  org: string,
  input: SetSSOInput,
): Promise<DataResult<SSOConfig>> {
  return sendData<SSOConfig>(token, "PUT", `/api/v1/orgs/${org}/sso`, input);
}

export function verifyOrgSSODomain(token: string, org: string): Promise<DataResult<SSOConfig>> {
  return postData<SSOConfig>(token, `/api/v1/orgs/${org}/sso/verify-domain`, {});
}

export function deleteOrgSSO(token: string, org: string): Promise<SimpleResult> {
  return deleteResource(token, `/api/v1/orgs/${org}/sso`);
}

// SCIM provisioning tokens. The raw token is returned only on creation.
export type SCIMToken = {
  id: string;
  name: string;
  createdAt: string;
  lastUsedAt?: string;
  token?: string;
};

export function getOrgSCIMTokens(
  token: string,
  org: string,
): Promise<Result<{ tokens: SCIMToken[]; baseUrl: string }>> {
  return authGet<{ tokens: SCIMToken[]; baseUrl: string }>(token, `/api/v1/orgs/${org}/scim/tokens`);
}

export function createOrgSCIMToken(token: string, org: string, name: string): Promise<DataResult<SCIMToken>> {
  return postData<SCIMToken>(token, `/api/v1/orgs/${org}/scim/tokens`, { name });
}

export function revokeOrgSCIMToken(token: string, org: string, id: string): Promise<SimpleResult> {
  return deleteResource(token, `/api/v1/orgs/${org}/scim/tokens/${id}`);
}

// listOrgs returns the organizations the authenticated user belongs to. Degrades
// to an empty list on any error so nav never blanks the shell.
export async function listOrgs(token: string): Promise<Organization[]> {
  try {
    const res = await fetch(`${API_BASE}/api/v1/orgs`, {
      headers: { Authorization: `Bearer ${token}` },
      cache: "no-store",
    });
    if (!res.ok) return [];
    const data = (await res.json()) as { organizations?: Organization[] };
    return data.organizations ?? [];
  } catch {
    return [];
  }
}
