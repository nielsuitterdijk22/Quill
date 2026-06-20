import { listGitTokens, listSSHKeys, listOrgMembers, listOrgInvitations } from "../../lib/api";
import { getToken, requireSession } from "../../lib/session";
import { AccountDangerZone } from "./AccountDangerZone";
import { ChangePasswordForm } from "./ChangePasswordForm";
import { EmailForm } from "./EmailForm";
import { ExportDataButton } from "./ExportDataButton";
import { GitTokenPanel } from "./GitTokenPanel";
import { OrgPanel } from "./OrgPanel";
import { ProfileForm } from "./ProfileForm";
import { SSHKeyPanel } from "./SSHKeyPanel";

const clerkEnabled = !!process.env.NEXT_PUBLIC_CLERK_PUBLISHABLE_KEY;

// SettingsPage lets the signed-in user edit their profile (display name) and mint
// or revoke personal git access tokens for cloning and pushing over HTTPS.
export default async function SettingsPage() {
  const user = await requireSession();
  const token = await getToken();
  const [tokensRes, sshRes, membersRes, invitationsRes] = await Promise.all([
    token
      ? listGitTokens(token)
      : Promise.resolve({ ok: false as const, status: 401, message: "Not signed in." }),
    token
      ? listSSHKeys(token)
      : Promise.resolve({ ok: false as const, status: 401, message: "Not signed in." }),
    token && clerkEnabled
      ? listOrgMembers(token)
      : Promise.resolve({ ok: false as const, status: 0, message: "" }),
    token && clerkEnabled
      ? listOrgInvitations(token)
      : Promise.resolve({ ok: false as const, status: 0, message: "" }),
  ]);

  return (
    <>
      <div className="top">
        <h1>Settings</h1>
      </div>

      <div className="panel form-narrow">
        <h2>Profile</h2>
        <div className="readme-body">
          <p className="subtle">
            Signed in as <span className="mono">{user.username}</span>
            {user.email && <> · {user.email}</>}
          </p>
        </div>
      </div>

      <ProfileForm displayName={user.displayName} />

      <EmailForm email={user.email} />

      {!process.env.NEXT_PUBLIC_CLERK_PUBLISHABLE_KEY && <ChangePasswordForm />}

      <GitTokenPanel
        tokens={tokensRes.ok ? tokensRes.data : []}
        loadError={tokensRes.ok ? undefined : tokensRes.message}
      />

      <SSHKeyPanel
        keys={sshRes.ok ? sshRes.data : []}
        loadError={sshRes.ok ? undefined : sshRes.message}
      />

      {clerkEnabled && (
        <OrgPanel
          members={membersRes.ok ? membersRes.data.members : []}
          invitations={invitationsRes.ok ? invitationsRes.data.invitations : []}
          membersError={membersRes.ok ? undefined : membersRes.message || undefined}
          invitationsError={invitationsRes.ok ? undefined : invitationsRes.message || undefined}
        />
      )}

      <ExportDataButton />

      <AccountDangerZone />
    </>
  );
}
