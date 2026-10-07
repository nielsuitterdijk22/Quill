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

// AdminOrgSummary is an org tenant with its SSO-link status, for the
// platform-admin console (lists every org, not just the caller's memberships).
export type AdminOrgSummary = {
  slug: string;
  name: string;
  ssoConfigured: boolean;
  externalOrgId: string;
  emailDomain: string;
};

// listAllOrgs lists every org tenant with its SSO status (platform admin only).
export function listAllOrgs(
  token: string,
): Promise<Result<{ organizations: AdminOrgSummary[] }>> {
  return authGet<{ organizations: AdminOrgSummary[] }>(token, "/api/v1/admin/orgs");
}

// createOrg provisions a new org tenant (with the caller as its first admin) plus
// a same-named project. The self-service path, used from onboarding.
export function createOrg(
  token: string,
  slug: string,
  name: string,
): Promise<DataResult<{ org: Organization }>> {
  return postData<{ org: Organization }>(token, "/api/v1/orgs", { slug, name });
}

// createTenant provisions a bare org tenant (platform admin only): no creator
// membership and no seed project. Used from the platform-admin console to onboard
// a customer whose members arrive by SSO/invite.
export function createTenant(
  token: string,
  slug: string,
  name: string,
): Promise<DataResult<{ org: Organization }>> {
  return postData<{ org: Organization }>(token, "/api/v1/admin/orgs", { slug, name });
}

// ProvisionSSOInput onboards an SSO customer in one platform-admin action: create
// a Quill tenant, provision its backing Zitadel org (IdP + ORG_OWNER admin shells
// + external-only login), link them, and pre-seed the admins.
export type ProvisionSSOInput = {
  slug: string;
  name: string;
  protocol: string; // 'oidc' | 'saml'
  issuer: string;
  clientId: string;
  clientSecret: string;
  emailDomain: string;
  adminEmails: string[];
};

export function provisionSSOTenant(
  token: string,
  input: ProvisionSSOInput,
): Promise<DataResult<SSOConfig>> {
  return postData<SSOConfig>(token, "/api/v1/admin/orgs/sso", input);
}

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

// SSOConfig is an org's SSO link to a hand-provisioned Zitadel org. Quill does no
// identity-provider provisioning; this just records which Zitadel org the
// customer's users authenticate against (plus the email domain for reference).
export type SSOConfig = {
  configured: boolean;
  externalOrgId: string;
  emailDomain: string;
  updatedAt: string;
  // Domain-ownership verification. domainVerified is live-read from Zitadel (the
  // source of truth); domainStatusKnown is false when that read failed, so the UI
  // can show "unknown" rather than falsely "pending". The record* fields are the
  // persisted DNS challenge to publish (empty until one is generated).
  domainVerified: boolean;
  domainStatusKnown: boolean;
  domainRecordType: string; // "dns" | "http" | ""
  domainRecordName: string;
  domainRecordValue: string;
};

export type SetSSOInput = {
  externalOrgId: string;
  emailDomain: string;
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

export function deleteOrgSSO(token: string, org: string): Promise<SimpleResult> {
  return deleteResource(token, `/api/v1/orgs/${org}/sso`);
}

// generateSSODomainValidation (re)issues the DNS ownership challenge for the org's
// email domain and returns the record to publish (platform admin only).
export function generateSSODomainValidation(
  token: string,
  org: string,
): Promise<DataResult<SSOConfig>> {
  return postData<SSOConfig>(token, `/api/v1/orgs/${org}/sso/domain/validation`, {});
}

// checkSSODomainVerification asks Zitadel to validate the outstanding challenge and
// returns the refreshed status (platform admin only).
export function checkSSODomainVerification(
  token: string,
  org: string,
): Promise<DataResult<SSOConfig>> {
  return postData<SSOConfig>(token, `/api/v1/orgs/${org}/sso/domain/verify`, {});
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
