import { loadOrg } from "./org";
import { SettingsNav } from "./SettingsNav";

// Organization settings: one page per section (members, groups, sign-in &
// provisioning, billing, policies) behind a shared header and sub-navigation.
// Membership is the access check; admins edit, members get read-only views.
export default async function OrgSettingsLayout({
  params,
  children,
}: {
  params: { org: string };
  children: React.ReactNode;
}) {
  const { org, canEdit } = await loadOrg(params.org);
  return (
    <>
      <div className="crumbs">
        <span>Organizations</span> <span>/</span> <span>{org.name}</span> <span>/</span> <span>Settings</span>
      </div>
      <div className="top">
        <h1>{org.name} settings</h1>
      </div>
      {!canEdit && (
        <div className="banner">You are a member of this organization. Only org admins can change these settings.</div>
      )}
      <div className="settings-layout">
        <SettingsNav org={org.slug} canEdit={canEdit} />
        <div className="settings-content">{children}</div>
      </div>
    </>
  );
}
