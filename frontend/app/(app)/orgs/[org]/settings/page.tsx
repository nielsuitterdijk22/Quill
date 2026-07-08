import Link from "next/link";
import { notFound } from "next/navigation";

import {
  getTenantEnvironmentPolicies,
  getTenantPolicies,
  listOrgs,
} from "../../../../lib/api";
import { getToken } from "../../../../lib/session";
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

  const [branchRes, envRes] = await Promise.all([
    getTenantPolicies(token, params.org),
    getTenantEnvironmentPolicies(token, params.org),
  ]);
  const branchPolicies = branchRes.ok ? branchRes.data.policies : [];
  const envPolicies = envRes.ok ? envRes.data.policies : [];

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
