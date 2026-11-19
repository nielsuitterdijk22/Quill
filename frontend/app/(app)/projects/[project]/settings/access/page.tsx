import Link from "next/link";
import { notFound } from "next/navigation";

import { ClaimOwnership } from "../../../../../components/project/ClaimOwnership";
import { ProjectAccessPanel } from "../../../../../components/project/ProjectAccessPanel";
import { getProjectAccess } from "../../../../../lib/api";
import { getToken } from "../../../../../lib/session";

// Who can reach a project. Being a member of the organization grants nothing:
// access is a direct grant or a group grant, managed by the project's owners.
export default async function ProjectAccessPage({
  params,
}: {
  params: { project: string };
}) {
  const token = await getToken();
  if (!token) notFound();

  const res = await getProjectAccess(token, params.project);
  if (!res.ok && res.status === 404) notFound();

  return (
    <>
      <div className="crumbs">
        <Link href="/projects">Projects</Link> <span>/</span>{" "}
        <Link href={`/projects/${params.project}`}>{params.project}</Link>{" "}
        <span>/</span> <span>Access</span>
      </div>
      <div className="top">
        <h1>Access</h1>
      </div>
      <p className="subtle">
        Membership of the organization does not give access to a project. Owners decide who gets in,
        directly or through a group. Roles: <b>owner</b> manages the project, <b>member</b> reads and
        writes, <b>read</b> can only look.
      </p>
      {res.ok ? (
        <ProjectAccessPanel project={params.project} access={res.data} />
      ) : (
        <>
          <div className="banner">
            {res.status === 403
              ? "Only the project's owners can manage access."
              : res.message}
          </div>
          {res.status === 403 && <ClaimOwnership project={params.project} />}
        </>
      )}
    </>
  );
}
