import Link from "next/link";
import { notFound } from "next/navigation";

import { getOrgInvites, getOrgMembers, getOrgSSO, listAllOrgs } from "../../../../lib/api";
import type { SSOConfig } from "../../../../lib/api";
import { getToken, requireSession } from "../../../../lib/session";
import { OrgMembers } from "../../../../components/organization/OrgMembers";
import { OrgSSO } from "../../../../components/organization/OrgSSO";

// AdminOrgDetailPage manages one customer org from the platform-admin console:
// link its SSO to a hand-provisioned Zitadel org, and hand off org admin (invite
// the customer's admin, or promote a member once they've signed in). Reachable by
// platform admins regardless of their own membership in the org.
export default async function AdminOrgDetailPage({
  params,
}: {
  params: { slug: string };
}) {
  const user = await requireSession();
  if (!user.isAdmin) notFound();

  const token = await getToken();
  if (!token) notFound();

  const [allRes, membersRes, invitesRes, ssoRes] = await Promise.all([
    listAllOrgs(token),
    getOrgMembers(token, params.slug),
    getOrgInvites(token, params.slug),
    getOrgSSO(token, params.slug),
  ]);

  const org = allRes.ok
    ? allRes.data.organizations.find((o) => o.slug === params.slug)
    : undefined;
  if (!org) notFound();

  const members = membersRes.ok ? membersRes.data.members : [];
  const invites = invitesRes.ok ? invitesRes.data.invites : [];
  const sso: SSOConfig = ssoRes.ok
    ? ssoRes.data
    : {
        configured: false,
        externalOrgId: "",
        emailDomain: "",
        updatedAt: "",
        domainVerified: false,
        domainStatusKnown: false,
        domainRecordType: "",
        domainRecordName: "",
        domainRecordValue: "",
      };

  return (
    <>
      <div className="crumbs">
        <Link href="/admin/organizations">Organizations</Link> <span>/</span>{" "}
        <span>{org.name}</span>
      </div>
      <div className="top">
        <h1>{org.name}</h1>
      </div>
      <p className="subtle mono">{org.slug}</p>

      <section className="settings-section settings-card">
        <div className="settings-head">
          <h2 className="settings-title">Single sign-on</h2>
          <p className="subtle">
            Link this org to a Zitadel org set up by hand in the console. Users who
            authenticate into that Zitadel org land in this workspace and are joined
            automatically.
          </p>
        </div>
        <OrgSSO org={org.slug} config={sso} />
      </section>

      <section className="settings-section settings-card">
        <div className="settings-head">
          <h2 className="settings-title">Members &amp; admin handoff</h2>
          <p className="subtle">
            Invite the customer&apos;s admin with the role set to <strong>Admin</strong>,
            or promote an existing member to Admin once they&apos;ve signed in via SSO.
          </p>
        </div>
        <OrgMembers org={org.slug} canEdit members={members} invites={invites} />
      </section>
    </>
  );
}
