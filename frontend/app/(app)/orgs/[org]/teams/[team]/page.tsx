import Link from "next/link";
import { notFound } from "next/navigation";

import { getTeam } from "../../../../../lib/api";
import { getToken } from "../../../../../lib/session";
import { TeamMembers } from "./TeamMembers";

// TeamDetailPage shows a team and its members, with controls to add and remove
// members. Authorization is enforced by the backend.
export default async function TeamDetailPage({
  params,
}: {
  params: { org: string; team: string };
}) {
  const token = getToken();
  if (!token) notFound();

  const result = await getTeam(token, params.org, params.team);
  if (!result.ok) {
    if (result.status === 404) notFound();
    return (
      <>
        <div className="crumbs">
          <Link href="/orgs">Organizations</Link> <span>/</span>{" "}
          <Link href={`/orgs/${params.org}`}>{params.org}</Link> <span>/</span>{" "}
          <Link href={`/orgs/${params.org}/teams`}>Teams</Link> <span>/</span>{" "}
          <span>{params.team}</span>
        </div>
        <h1>{params.team}</h1>
        <div className="banner">
          {result.status === 403
            ? "You are not a member of this organization."
            : result.message}
        </div>
      </>
    );
  }

  const { organization: org, team, members } = result.data;

  return (
    <>
      <div className="crumbs">
        <Link href="/orgs">Organizations</Link> <span>/</span>{" "}
        <Link href={`/orgs/${org.slug}`}>{org.slug}</Link> <span>/</span>{" "}
        <Link href={`/orgs/${org.slug}/teams`}>Teams</Link> <span>/</span>{" "}
        <span>{team.slug}</span>
      </div>

      <h1>{team.name}</h1>
      {team.description && <p className="subtle">{team.description}</p>}

      <TeamMembers org={org.slug} team={team.slug} members={members} />
    </>
  );
}
