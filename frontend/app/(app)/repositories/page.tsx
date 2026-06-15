import Link from "next/link";
import { notFound } from "next/navigation";

import { getToken } from "../../lib/session";
import { listOrgs, listReposByOrg } from "../../lib/api";
import { OrgFilter } from "./OrgFilter";

export default async function ReposPage({
  searchParams,
}: {
  searchParams: { org?: string };
}) {
  const token = getToken();
  if (!token) notFound();

  // The set of orgs the user is authorized for. This list (and the per-org
  // repo endpoint below) is what authorizes the view: the backend only returns
  // orgs the caller is a member of, so a crafted ?org= for some other org is
  // never honoured — we scope to the requested org only when it is present in
  // this authorized set, otherwise we fall back to the first authorized org.
  const orgs = await listOrgs(token);
  if (orgs.length === 0) notFound();

  const requested = searchParams.org;
  const selected =
    (requested && orgs.find((o) => o.slug === requested)) || orgs[0];

  const repos = await listReposByOrg(token, selected.slug);

  return (
    <>
      <div className="top">
        <h1>Repositories</h1>
        <Link
          className="btn primary"
          href={`/orgs/${selected.slug}/repos/new`}
        >
          + New Repository
        </Link>
      </div>

      <div className="panel">
        <div className="panel-toolbar">
          <OrgFilter orgs={orgs} selected={selected.slug} />
        </div>
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
              href={`/orgs/${selected.slug}/repos/${o.slug}`}
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
