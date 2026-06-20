import { notFound } from "next/navigation";

import { getBranches } from "../../../../../../../lib/api";
import { getToken } from "../../../../../../../lib/session";
import { BrowseError, RepoHeader } from "../../../../../../../components/repo";
import { NewPullForm } from "./NewPullForm";

// NewPullPage renders the open-a-pull-request form with the repository's
// branches available as the source/target options.
export default async function NewPullPage({
  params,
}: {
  params: { project: string; repo: string };
}) {
  const token = await getToken();
  if (!token) notFound();

  const result = await getBranches(token, params.project, params.repo);
  if (!result.ok) {
    if (result.status === 404) notFound();
    return (
      <BrowseError
        project={params.project}
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
        project={params.project}
        repo={params.repo}
        visibility={repo.visibility}
        refName={defaultBranch}
        active="pulls"
      />
      <NewPullForm
        project={params.project}
        repo={params.repo}
        branches={branches}
        defaultBranch={defaultBranch}
      />
    </>
  );
}
