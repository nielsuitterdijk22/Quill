import { notFound } from "next/navigation";

import { getRepo } from "../../../../../../../lib/api";
import { getToken } from "../../../../../../../lib/session";
import { BrowseError, RepoHeader } from "../../../../../../../components/repo";
import { NewIssueForm } from "./NewIssueForm";

export default async function NewIssuePage({
  params,
}: {
  params: { project: string; repo: string };
}) {
  const token = await getToken();
  if (!token) notFound();

  const result = await getRepo(token, params.project, params.repo);
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

  const repo = result.data;

  return (
    <>
      <RepoHeader
        project={params.project}
        repo={params.repo}
        visibility={repo.visibility}
        refName={repo.defaultBranch}
        active="issues"
      />
      <NewIssueForm project={params.project} repo={params.repo} />
    </>
  );
}
