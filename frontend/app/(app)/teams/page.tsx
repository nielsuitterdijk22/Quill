import Link from "next/link";

import { getMyTeams } from "../../lib/api";
import { getToken } from "../../lib/session";

// TeamsPage lists every team the signed-in user belongs to, across all of their
// organizations, linking each to the org-scoped team detail page.
export default async function TeamsPage() {
  const token = getToken();
  const teams = token ? await getMyTeams(token) : [];

  return (
    <>
      <div className="top">
        <h1>Teams</h1>
      </div>

      <div className="panel">
        <h2>
          Your teams
          <span className="tag">{teams.length}</span>
        </h2>
        {teams.length === 0 ? (
          <div className="empty">
            You are not a member of any team yet. Open an organization and create
            or join a team from its Teams page.
          </div>
        ) : (
          teams.map((t) => (
            <Link
              className="row-item"
              key={t.id}
              href={`/orgs/${t.orgSlug}/teams/${t.slug}`}
            >
              <span className="tree-icon dir">◎</span>
              <span className="nm">{t.name}</span>
              <span className="sub">
                · {t.orgSlug}/{t.slug}
              </span>
              <span className="tag">{t.role}</span>
              <span className="spacer" />
            </Link>
          ))
        )}
      </div>
    </>
  );
}
