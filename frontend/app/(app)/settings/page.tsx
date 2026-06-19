import { listGitTokens } from "../../lib/api";
import { getToken, requireSession } from "../../lib/session";
import { ChangePasswordForm } from "./ChangePasswordForm";
import { GitTokenPanel } from "./GitTokenPanel";
import { ProfileForm } from "./ProfileForm";

// SettingsPage lets the signed-in user edit their profile (display name) and mint
// or revoke personal git access tokens for cloning and pushing over HTTPS.
export default async function SettingsPage() {
  const user = await requireSession();
  const token = getToken();
  const tokens = token ? await listGitTokens(token) : [];

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

      <ChangePasswordForm />

      <GitTokenPanel tokens={tokens} />
    </>
  );
}
