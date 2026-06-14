import { notFound } from "next/navigation";

import { getBranchPolicies, getBranches } from "../../../../../lib/api";
import { getToken } from "../../../../../lib/session";
import { BrowseError, RepoHeader } from "../../../../../components/repo";
import { PolicyManager } from "./PolicyManager";

// RepoSettingsPage manages a repository's branch policies — the protection rules
// (required approvals, pull-request enforcement) Quill stores and enforces.
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

      <div className="settings-head">
        <h1 className="settings-title">Branch policies</h1>
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
    </>
  );
}
