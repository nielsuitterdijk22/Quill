import Link from "next/link";

import { listOrgs } from "../../lib/api";
import { getToken } from "../../lib/session";

// OrgsPage lists every organization the signed-in user can see, linking each to
// its repository list. Org creation is open to any authenticated user.
export default async function OrgsPage() {
  const token = getToken();
  const orgs = token ? await listOrgs(token) : [];

  return (
    <>
      <div className="top">
        <h1>Organizations</h1>
        <Link className="btn primary" href="/orgs/new">
          ＋ New organization
        </Link>
      </div>

      <div className="panel">
        <h2>
          All organizations
          <span className="tag">{orgs.length}</span>
        </h2>
        {orgs.length === 0 ? (
          <div className="empty">
            No organizations yet. Create one — it is mirrored into Forgejo and
            gets a default <span className="mono">owners</span> team.
          </div>
        ) : (
          orgs.map((o) => (
            <Link className="row-item" key={o.id} href={`/orgs/${o.slug}`}>
              <span className="tree-icon dir">◆</span>
              <span className="nm">{o.name}</span>
              <span className="sub">· {o.slug}</span>
              <span className="spacer" />
              {o.forgejoOrg ? (
                <span className="tag">forgejo</span>
              ) : (
                <span className="tag">local</span>
              )}
            </Link>
          ))
        )}
      </div>
    </>
  );
}
