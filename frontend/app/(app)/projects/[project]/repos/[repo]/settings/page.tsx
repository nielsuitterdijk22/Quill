import { notFound } from "next/navigation";

import { getBranchPolicies, getBranches } from "../../../../../../lib/api";
import { getToken } from "../../../../../../lib/session";
import { BrowseError, RepoHeader } from "../../../../../../components/repo";
import { PolicyManager } from "../../../../../../components/policy/PolicyManager";
import { DangerZone } from "./DangerZone";
import { RepoSettingsForm } from "./RepoSettingsForm";

// RepoSettingsPage manages a repository's configuration: general metadata
// (name, description, visibility, default branch), the branch protection rules
// Quill enforces, and the irreversible danger-zone operations.
export default async function RepoSettingsPage({
  params,
}: {
  params: { project: string; repo: string };
}) {
  const token = getToken();
  if (!token) notFound();

  const policiesRes = await getBranchPolicies(token, params.project, params.repo);
  if (!policiesRes.ok) {
    if (policiesRes.status === 404) notFound();
    return (
      <BrowseError
        project={params.project}
        repo={params.repo}
        status={policiesRes.status}
        message={policiesRes.message}
      />
    );
  }

  const { repository: repo, policies, inherited } = policiesRes.data;
  const branchesRes = await getBranches(token, params.project, params.repo);
  const branchNames = branchesRes.ok
    ? branchesRes.data.branches.map((b) => b.name)
    : [];

  return (
    <>
      <RepoHeader
        project={params.project}
        repo={params.repo}
        visibility={repo.visibility}
        refName={repo.defaultBranch}
        active="settings"
      />

      {repo.isArchived && (
        <div className="banner">
          This repository is archived and read-only. Unarchive it below to make
          changes.
        </div>
      )}

      <section className="settings-section">
        <div className="settings-head">
          <h1 className="settings-title">General</h1>
          <p className="subtle">
            Update how this repository is named and who can see it.
          </p>
        </div>
        <RepoSettingsForm
          project={params.project}
          repo={params.repo}
          repository={repo}
          branchNames={branchNames}
        />
      </section>

      <section className="settings-section">
        <div className="settings-head">
          <h2 className="settings-title">Branch policies</h2>
          <p className="subtle">
            Protect branches by requiring reviews before a pull request can
            merge. Quill enforces these rules on every merge.
          </p>
        </div>
        <PolicyManager
          target={{ scope: "repo", project: params.project, repo: params.repo }}
          policies={policies}
          inherited={inherited}
          branchNames={branchNames}
          defaultBranch={repo.defaultBranch}
        />
      </section>

      <section className="settings-section">
        <div className="settings-head">
          <h2 className="settings-title danger">Danger zone</h2>
          <p className="subtle">
            Renaming, archiving, and deletion affect everyone with access.
          </p>
        </div>
        <DangerZone
          project={params.project}
          repo={params.repo}
          archived={repo.isArchived}
          visibility={repo.visibility}
        />
      </section>
    </>
  );
}
