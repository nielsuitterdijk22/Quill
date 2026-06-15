import { notFound } from "next/navigation";

import { getBranchPolicies, getBranches } from "../../../../../lib/api";
import { getToken } from "../../../../../lib/session";
import { BrowseError, RepoHeader } from "../../../../../components/repo";
import { DangerZone } from "./DangerZone";
import { PolicyManager } from "./PolicyManager";
import { RepoSettingsForm } from "./RepoSettingsForm";

// RepoSettingsPage manages a repository's configuration: general metadata
// (name, description, visibility, default branch), the branch protection rules
// Quill enforces, and the irreversible danger-zone operations.
export default async function RepoSettingsPage({
  params,
}: {
  params: { org: string; repo: string };
}) {
  const token = getToken();
  if (!token) notFound();

  const policiesRes = await getBranchPolicies(token, params.org, params.repo);
  if (!policiesRes.ok) {
    if (policiesRes.status === 404) notFound();
    return (
      <BrowseError
        org={params.org}
        repo={params.repo}
        status={policiesRes.status}
        message={policiesRes.message}
      />
    );
  }

  const { repository: repo, policies } = policiesRes.data;
  const branchesRes = await getBranches(token, params.org, params.repo);
  const branchNames = branchesRes.ok
    ? branchesRes.data.branches.map((b) => b.name)
    : [];

  return (
    <>
      <RepoHeader
        org={params.org}
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
          org={params.org}
          repo={params.repo}
          repository={repo}
          branchNames={branchNames}
        />
      </section>

      <section className="settings-section">
        <div className="settings-head">
          <h2 className="settings-title">Branch policies</h2>
          <p className="subtle">
            Protect branches by requiring reviews before a pull request can merge.
            Quill enforces these rules on every merge.
          </p>
        </div>
        <PolicyManager
          org={params.org}
          repo={params.repo}
          policies={policies}
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
          org={params.org}
          repo={params.repo}
          archived={repo.isArchived}
        />
      </section>
    </>
  );
}
