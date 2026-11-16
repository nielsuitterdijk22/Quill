import Link from "next/link";
import { notFound } from "next/navigation";

import {
  getOrgBilling,
  getOrgGroups,
  getOrgInvites,
  getOrgMembers,
  getOrgSCIMTokens,
  getOrgSSO,
  getTenantEnvironmentPolicies,
  getTenantEgress,
  getTenantPolicies,
  listOrgs,
} from "../../../../lib/api";
import type { BillingView, SSOConfig } from "../../../../lib/api";
import { getToken } from "../../../../lib/session";
import { OrgBilling } from "../../../../components/organization/OrgBilling";
import { OrgGroups } from "../../../../components/organization/OrgGroups";
import { OrgMembers } from "../../../../components/organization/OrgMembers";
import { OrgSSO } from "../../../../components/organization/OrgSSO";
import { OrgProvisioning } from "../../../../components/organization/OrgProvisioning";
import { PolicyManager } from "../../../../components/policy/PolicyManager";
import { EnvironmentPolicyManager } from "../../../../components/policy/EnvironmentPolicyManager";
import { TenantEgressForm } from "../../../../components/egress/EgressForms";

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

  const [branchRes, envRes, membersRes, invitesRes, groupsRes, billingRes, egress] = await Promise.all([
    getTenantPolicies(token, params.org),
    getTenantEnvironmentPolicies(token, params.org),
    getOrgMembers(token, params.org),
    // Invites are admin-only; a plain member's 403 degrades to an empty list.
    canEdit ? getOrgInvites(token, params.org) : Promise.resolve({ ok: false as const }),
    getOrgGroups(token, params.org),
    // Billing is admin-only; a plain member's 403 degrades to undefined.
    canEdit ? getOrgBilling(token, params.org) : Promise.resolve({ ok: false as const }),
    getTenantEgress(token, params.org),
  ]);
  const branchPolicies = branchRes.ok ? branchRes.data.policies : [];
  const envPolicies = envRes.ok ? envRes.data.policies : [];
  const members = membersRes.ok ? membersRes.data.members : [];
  const invites = invitesRes.ok ? invitesRes.data.invites : [];
  const groups = groupsRes.ok ? groupsRes.data.groups : [];
  const billing: BillingView | undefined = billingRes.ok ? billingRes.data : undefined;

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

  // SCIM tokens are admin-only too.
  const scimRes = canEdit ? await getOrgSCIMTokens(token, params.org) : ({ ok: false } as const);
  const scimTokens = scimRes.ok ? scimRes.data.tokens : [];
  const scimBaseUrl = scimRes.ok ? scimRes.data.baseUrl : "";

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
              Let people sign in with your company identity provider (Okta, Microsoft
              Entra ID, Google Workspace, …). Once your email domain is verified,
              anyone signing in with an address on it goes straight to your company
              login and joins this organization. The client secret is stored
              encrypted and never shown again.
            </p>
          </div>
          <OrgSSO org={org.slug} config={sso} />
        </section>
      )}

      {canEdit && (
        <section className="settings-section settings-card">
          <div className="settings-head">
            <h2 className="settings-title">Provisioning (SCIM)</h2>
            <p className="subtle">
              Let your identity provider (Okta, Microsoft Entra ID, JumpCloud, …) manage members and groups.
              People it assigns join this organization, groups it pushes appear under Groups, and anyone it
              deactivates loses access immediately. Accounts are matched by email; people sign in with their
              usual Quill login.
            </p>
          </div>
          <OrgProvisioning
            org={org.slug}
            baseUrl={scimBaseUrl}
            tokens={scimTokens}
            unavailable={billing?.locked ? "Provisioning new members is paused until this organization subscribes; deprovisioning keeps working." : undefined}
          />
        </section>
      )}

      <section className="settings-section settings-card">
        <div className="settings-head">
          <h2 className="settings-title">Groups</h2>
          <p className="subtle">
            Departments and teams as a nested hierarchy (max depth 5). Grant a
            group a role on a project and every member — including members of
            its subgroups — inherits that access.
          </p>
        </div>
        <OrgGroups org={org.slug} canEdit={canEdit} groups={groups} members={members} />
      </section>

      {billing && (
        <section className="settings-section settings-card">
          <div className="settings-head">
            <h2 className="settings-title">Billing &amp; usage</h2>
            <p className="subtle">
              Your plan and metered usage. The plan is set by the platform
              operator; overage and invoicing follow the plan.
            </p>
          </div>
          <OrgBilling org={org.slug} billing={billing} />
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

      {egress && (
        <section className="settings-section settings-card" id="egress">
          <div className="settings-head">
            <h2 className="settings-title">Pipeline egress</h2>
            <p className="subtle">
              Which hosts pipelines may reach. Every repository in this organization inherits this policy and can
              add hosts of its own.
            </p>
          </div>
          <TenantEgressForm policy={egress} tenant={org.slug} path={`/orgs/${org.slug}/settings`} />
        </section>
      )}
    </>
  );
}
