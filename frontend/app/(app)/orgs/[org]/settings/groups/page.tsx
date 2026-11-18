import {
  getGroupMembers,
  getGroupProjects,
  getOrgGroups,
  getOrgMembers,
  getOrgProjects,
} from "../../../../../lib/api";
import { GroupExplorer } from "../../../../../components/organization/GroupExplorer";
import { loadOrg } from "../org";

export default async function OrgGroupsSettings({
  params,
  searchParams,
}: {
  params: { org: string };
  searchParams: { group?: string };
}) {
  const { token, org, canEdit } = await loadOrg(params.org);
  const [groupsRes, membersRes, projectsRes] = await Promise.all([
    getOrgGroups(token, params.org),
    getOrgMembers(token, params.org),
    getOrgProjects(token, params.org),
  ]);
  const groups = groupsRes.ok ? groupsRes.data.groups : [];
  const selected =
    groups.find((g) => g.slug === searchParams.group) ??
    (searchParams.group ? null : groups[0] ?? null);
  const [gm, gp] = selected
    ? await Promise.all([getGroupMembers(token, params.org, selected.slug), getGroupProjects(token, params.org, selected.slug)])
    : [null, null];
  return (
    <section className="settings-section settings-card">
      <div className="settings-head">
        <h2 className="settings-title">Groups</h2>
        <p className="subtle">
          A group is a named set of people. Grant a group a role on a project and everyone in it gets that access.
          Groups pushed by your identity provider (SCIM) appear here too.
        </p>
      </div>
      <GroupExplorer
        org={org.slug}
        canEdit={canEdit}
        groups={groups}
        members={membersRes.ok ? membersRes.data.members : []}
        projects={projectsRes.ok ? projectsRes.data.projects : []}
        selected={selected}
        selectedMembers={gm && gm.ok ? gm.data.members : []}
        selectedProjects={gp && gp.ok ? gp.data.projects : []}
      />
    </section>
  );
}
