import Link from "next/link";

import { getMyProjects, listGitTokens, listSSHKeys } from "../../lib/api";
import type { User } from "../../lib/api";
import { getToken, requireSession } from "../../lib/session";
import {
  fetchProjectGovernance,
  GovernancePolicies,
  GovernanceSecretsEnvironments,
} from "../../components/settings/ProjectGovernanceSettings";
import { AccountDangerZone } from "./AccountDangerZone";
import { ChangePasswordForm } from "./ChangePasswordForm";
import { EmailForm } from "./EmailForm";
import { ExportDataButton } from "./ExportDataButton";
import { GitTokenPanel } from "./GitTokenPanel";
import { ProfileForm } from "./ProfileForm";
import { SSHKeyPanel } from "./SSHKeyPanel";

// SettingsPage is the signed-in user's settings home. It has up to three tabs:
//   * Account — profile, email, password, git tokens, SSH keys, data export.
//   * Policies — branch and environment policies for the user's personal
//     project, which is their global ("all my repos") scope.
//   * Secrets & environments — secrets and deployment environments for the same.
// The Policies and Secrets tabs only appear for accounts that have a personal
// project (individuals). Org members manage the same settings per project under
// /projects/{slug}/settings, reached via the Projects nav.
type Tab = "account" | "policies" | "secrets";

export default async function SettingsPage({
  searchParams,
}: {
  searchParams: { tab?: string };
}) {
  const user = await requireSession();
  const token = await getToken();

  const projects = token ? await getMyProjects(token) : [];
  const personalProject = projects.find((p) => p.isPersonal);

  // The governance tabs only exist when the user has a personal project.
  const requested = searchParams.tab;
  const tab: Tab =
    personalProject && (requested === "policies" || requested === "secrets")
      ? requested
      : "account";

  return (
    <>
      <div className="top">
        <h1>Settings</h1>
      </div>

      {personalProject && (
        <div className="repo-toolbar">
          <div className="state-tabs">
            <Link className={tab === "account" ? "active" : ""} href="/settings">
              <span className="ic">⚙</span> Account
            </Link>
            <Link
              className={tab === "policies" ? "active" : ""}
              href="/settings?tab=policies"
            >
              <span className="ic">🛡</span> Policies
            </Link>
            <Link
              className={tab === "secrets" ? "active" : ""}
              href="/settings?tab=secrets"
            >
              <span className="ic">🔑</span> Secrets &amp; environments
            </Link>
          </div>
        </div>
      )}

      {tab === "account" && <AccountTab user={user} token={token} />}
      {tab === "policies" && personalProject && (
        <GovernanceTab token={token} project={personalProject.slug} kind="policies" />
      )}
      {tab === "secrets" && personalProject && (
        <GovernanceTab token={token} project={personalProject.slug} kind="secrets" />
      )}
    </>
  );
}

// AccountTab renders the user's account settings: profile, email, password, git
// access tokens for HTTPS clone/push, SSH keys, data export, and account
// deletion.
async function AccountTab({
  user,
  token,
}: {
  user: User;
  token: string | undefined;
}) {
  const [tokensRes, sshRes] = await Promise.all([
    token
      ? listGitTokens(token)
      : Promise.resolve({ ok: false as const, status: 401, message: "Not signed in." }),
    token
      ? listSSHKeys(token)
      : Promise.resolve({ ok: false as const, status: 401, message: "Not signed in." }),
  ]);

  return (
    <>
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

      <ChangePasswordForm />

      <GitTokenPanel
        tokens={tokensRes.ok ? tokensRes.data : []}
        loadError={tokensRes.ok ? undefined : tokensRes.message}
      />

      <SSHKeyPanel
        keys={sshRes.ok ? sshRes.data : []}
        loadError={sshRes.ok ? undefined : sshRes.message}
      />

      <ExportDataButton />

      <AccountDangerZone />
    </>
  );
}

// GovernanceTab renders one slice of the personal project's governance — either
// its policies or its secrets and environments — the same managers org users get
// on a project settings page.
async function GovernanceTab({
  token,
  project,
  kind,
}: {
  token: string | undefined;
  project: string;
  kind: "policies" | "secrets";
}) {
  if (!token) {
    return <div className="banner">Your session has expired. Sign in again.</div>;
  }
  const governance = await fetchProjectGovernance(token, project);
  if (!governance.ok) {
    return <div className="banner">{governance.message}</div>;
  }

  if (kind === "policies") {
    return (
      <>
        <p className="subtle form-narrow">
          Branch and deployment policies for your personal namespace. They apply
          to every repository you own here; a repository may add stricter rules
          but cannot weaken these.
        </p>
        <GovernancePolicies data={governance.data} />
      </>
    );
  }

  return (
    <>
      <p className="subtle form-narrow">
        Secrets and deployment environments for your personal namespace. Secrets
        are exposed to workflows as{" "}
        <span className="mono">${"{{ secrets.NAME }}"}</span>.
      </p>
      <GovernanceSecretsEnvironments data={governance.data} />
    </>
  );
}
