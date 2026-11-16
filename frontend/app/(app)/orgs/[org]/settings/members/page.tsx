import { getOrgInvites, getOrgMembers } from "../../../../../lib/api";
import { OrgMembers } from "../../../../../components/organization/OrgMembers";
import { loadOrg } from "../org";

export default async function OrgMembersSettings({ params }: { params: { org: string } }) {
  const { token, org, canEdit } = await loadOrg(params.org);
  const [membersRes, invitesRes] = await Promise.all([
    getOrgMembers(token, params.org),
    canEdit ? getOrgInvites(token, params.org) : Promise.resolve({ ok: false as const }),
  ]);
  return (
    <section className="settings-section settings-card">
      <div className="settings-head">
        <h2 className="settings-title">Members</h2>
        <p className="subtle">
          People with access to this organization. Admins manage settings, members and org-wide policies.
          {canEdit ? " Invite by email — the invitee gets a link to accept." : ""}
        </p>
      </div>
      <OrgMembers
        org={org.slug}
        canEdit={canEdit}
        members={membersRes.ok ? membersRes.data.members : []}
        invites={invitesRes.ok ? invitesRes.data.invites : []}
      />
    </section>
  );
}
