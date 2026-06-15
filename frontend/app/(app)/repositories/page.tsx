import Link from "next/link";
import { notFound } from "next/navigation";

import { getDefaultOrg } from "../../lib/orgs";
import { getToken } from "../../lib/session";
import { listReposByOrg } from "../../lib/api";

export default async function ReposPage() {
  const token = getToken();
  if (!token) notFound();

  const org = await getDefaultOrg();
  if (!org) notFound();

  const repos = await listReposByOrg(token, org);

  return (
    <>
      <div className="top">
        <h1>Repositories in {org}</h1>
        <Link className="btn primary" href={`/orgs/${org}/repos/new`}>
          + New Repository
          {/* // TOOD: repos/new? ＋ New Repository */}
        </Link>
      </div>

      <div className="panel">
        <h2>
          All Repositories
          <span className="tag">{repos.length}</span>
        </h2>
        {repos.length === 0 ? (
          <div className="empty">No repositories yet. Create one :D</div>
        ) : (
          repos.map((o) => (
            <Link
              className="row-item"
              key={o.id}
              href={`/orgs/${org}/repos/${o.slug}`}
            >
              <span className="tree-icon dir">◆</span>
              <span className="nm">{o.name}</span>
              <span className="sub">· {o.slug}</span>
              <span className="spacer" />
            </Link>
          ))
        )}
      </div>
    </>
  );
}
