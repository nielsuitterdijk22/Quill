import Link from "next/link";
import { notFound } from "next/navigation";

import { getTeamsByOrg } from "../../../../lib/api";
import { getToken } from "../../../../lib/session";

// OrgTeamsPage lists the teams in an organization. Membership is enforced by the
// backend; a 403 renders a no-access notice, a 404 is a real not-found.
export default async function OrgTeamsPage({
  params,
}: {
  params: { org: string };
}) {
  const token = getToken();
  if (!token) notFound();

  const result = await getTeamsByOrg(token, params.org);
  if (!result.ok) {
    if (result.status === 404) notFound();
    return (
      <>
        <div className="crumbs">
          <Link href="/orgs">Organizations</Link> <span>/</span>{" "}
          <Link href={`/orgs/${params.org}`}>{params.org}</Link> <span>/</span>{" "}
          <span>Teams</span>
        </div>
        <h1>Teams</h1>
        <div className="banner">
          {result.status === 403
            ? "You are not a member of this organization."
            : result.message}
        </div>
      </>
    );
  }

  const { organization: org, teams } = result.data;

  return (
    <>
      <div className="crumbs">
        <Link href="/orgs">Organizations</Link> <span>/</span>{" "}
        <Link href={`/orgs/${org.slug}`}>{org.slug}</Link> <span>/</span>{" "}
        <span>Teams</span>
      </div>

      <div className="top">
        <h1>Teams in {org.name}</h1>
        <Link className="btn primary" href={`/orgs/${org.slug}/teams/new`}>
          ＋ New team
        </Link>
      </div>

      <div className="panel">
        <h2>
          Teams
          <span className="tag">{teams.length}</span>
        </h2>
        {teams.length === 0 ? (
          <div className="empty">
            No teams yet. Create one to group members and own repositories.
          </div>
        ) : (
          teams.map((t) => (
            <Link
              className="row-item"
              key={t.id}
              href={`/orgs/${org.slug}/teams/${t.slug}`}
            >
              <span className="tree-icon dir">◎</span>
              <span className="nm">{t.name}</span>
              <span className="sub">· {t.slug}</span>
              <span className="spacer" />
            </Link>
          ))
        )}
      </div>
    </>
  );
}
