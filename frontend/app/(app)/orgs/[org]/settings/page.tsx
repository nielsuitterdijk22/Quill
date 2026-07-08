import Link from "next/link";
import { notFound } from "next/navigation";

import {
  getOrgInvites,
  getOrgMembers,
  getOrgSSO,
  getTenantEnvironmentPolicies,
  getTenantPolicies,
  listOrgs,
} from "../../../../lib/api";
import type { SSOConfig } from "../../../../lib/api";
import { getToken } from "../../../../lib/session";
import { OrgMembers } from "../../../../components/organization/OrgMembers";
import { OrgSSO } from "../../../../components/organization/OrgSSO";
import { PolicyManager } from "../../../../components/policy/PolicyManager";
import { EnvironmentPolicyManager } from "../../../../components/policy/EnvironmentPolicyManager";

// OrgSettingsPage is the management surface for an organization (an org-kind
// tenant): org-wide branch and environment policies today, with members (PR C)
// and SSO (PR D) landing alongside. Org admins may edit; plain members get a
// read-only view. Membership and role come from the caller's org list, which is
// also the access check — a non-member sees notFound.
export default async function OrgSettingsPage({
  params,
}: {
  params: { org: string };
}) {
  const token = await getToken();
  if (!token) notFound();

  const orgs = await listOrgs(token);
  const org = orgs.find((o) => o.slug === params.org);
  if (!org) notFound();
  const canEdit = org.role === "admin";

  const [branchRes, envRes, membersRes, invitesRes] = await Promise.all([
    getTenantPolicies(token, params.org),
    getTenantEnvironmentPolicies(token, params.org),
    getOrgMembers(token, params.org),
    // Invites are admin-only; a plain member's 403 degrades to an empty list.
    canEdit ? getOrgInvites(token, params.org) : Promise.resolve({ ok: false as const }),
  ]);
  const branchPolicies = branchRes.ok ? branchRes.data.policies : [];
  const envPolicies = envRes.ok ? envRes.data.policies : [];
  const members = membersRes.ok ? membersRes.data.members : [];
  const invites = invitesRes.ok ? invitesRes.data.invites : [];

  // SSO is admin-only; default to an empty (unconfigured) view for members.
  const ssoRes = canEdit
    ? await getOrgSSO(token, params.org)
    : ({ ok: false } as const);
  const sso: SSOConfig = ssoRes.ok
    ? ssoRes.data
    : {
        configured: false,
        protocol: "oidc",
        issuer: "",
        clientId: "",
        emailDomain: "",
        enabled: false,
        hasSecret: false,
        updatedAt: "",
      };

  return (
    <>
      <div className="crumbs">
        <span>Organizations</span> <span>/</span> <span>{org.name}</span>{" "}
        <span>/</span> <span>Settings</span>
      </div>

      <div className="top">
        <h1>{org.name} settings</h1>
      </div>

      {!canEdit && (
        <div className="banner">
          You are a member of this organization. Only org admins can change these
          settings.
        </div>
      )}

      <section className="settings-section settings-card">
        <div className="settings-head">
          <h2 className="settings-title">Members</h2>
          <p className="subtle">
            People with access to this organization. Admins manage settings,
            members, and org-wide policies.
            {canEdit
              ? " Invite by email — the invitee gets a link (and an email when SSO is configured)."
              : ""}
          </p>
        </div>
        <OrgMembers
          org={org.slug}
          canEdit={canEdit}
          members={members}
          invites={invites}
        />
      </section>

      {canEdit && (
        <section className="settings-section settings-card">
          <div className="settings-head">
            <h2 className="settings-title">Single sign-on</h2>
            <p className="subtle">
              Configure how members of this organization authenticate. The client
              secret is stored encrypted and never shown again. Routing logins by
              email domain is applied by the identity provider.
            </p>
          </div>
          <OrgSSO org={org.slug} config={sso} />
        </section>
      )}

      <section className="settings-section settings-card">
        <div className="settings-head">
          <h2 className="settings-title">Organization branch policies</h2>
          <p className="subtle">
            Rules set here apply to every repository in every project in this
            organization. A project or repository may add stricter rules but
            cannot weaken these. Lock a rule to forbid loosening it.
          </p>
        </div>
        <PolicyManager
          target={{ scope: "tenant", tenant: org.slug }}
          policies={branchPolicies}
          canLock
          canEdit={canEdit}
        />
      </section>

      <section className="settings-section settings-card">
        <div className="settings-head">
          <h2 className="settings-title">Organization environment policies</h2>
          <p className="subtle">
            Gate deploys for every repository in this organization. Projects and
            repositories may add stricter gates but cannot weaken these. Lock a
            gate to forbid loosening it.
          </p>
        </div>
        <EnvironmentPolicyManager
          target={{ scope: "tenant", tenant: org.slug }}
          policies={envPolicies}
          canLock
          canEdit={canEdit}
        />
      </section>
    </>
  );
}
