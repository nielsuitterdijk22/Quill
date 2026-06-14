import { notFound } from "next/navigation";

import { getBranches } from "../../../../../../lib/api";
import { getToken } from "../../../../../../lib/session";
import { BrowseError, RepoHeader } from "../../../../../../components/repo";
import { NewPullForm } from "./NewPullForm";

// NewPullPage renders the open-a-pull-request form with the repository's
// branches available as the source/target options.
export default async function NewPullPage({
  params,
}: {
  params: { org: string; repo: string };
}) {
  const token = getToken();
  if (!token) notFound();

  const result = await getBranches(token, params.org, params.repo);
  if (!result.ok) {
    if (result.status === 404) notFound();
    return (
      <BrowseError
        org={params.org}
        repo={params.repo}
        status={result.status}
        message={result.message}
      />
    );
  }

  const { repository: repo, defaultBranch, branches } = result.data;

  return (
    <>
      <RepoHeader
        org={params.org}
        repo={params.repo}
        visibility={repo.visibility}
        refName={defaultBranch}
        active="pulls"
      />
      <NewPullForm
        org={params.org}
        repo={params.repo}
        branches={branches}
        defaultBranch={defaultBranch}
      />
    </>
  );
}
