import Link from "next/link";
import { notFound } from "next/navigation";

import { getReposByOrg } from "../../../lib/api";
import { getToken } from "../../../lib/session";
import { VisibilityBadge } from "../../../components/repo";

// OrgDetailPage lists the repositories in an organization. Membership is enforced
// by the backend; a 403 renders a no-access notice, a 404 is a real not-found.
export default async function OrgDetailPage({
  params,
}: {
  params: { org: string };
}) {
  const token = getToken();
  if (!token) notFound();

  const result = await getReposByOrg(token, params.org);
  if (!result.ok) {
    if (result.status === 404) notFound();
    return (
      <>
        <div className="crumbs">
          <Link href="/orgs">Organizations</Link> <span>/</span>{" "}
          <span>{params.org}</span>
        </div>
        <h1>{params.org}</h1>
        <div className="banner">
          {result.status === 403
            ? "You are not a member of this organization."
            : result.message}
        </div>
      </>
    );
  }

  const { organization: org, repositories: repos } = result.data;

  return (
    <>
      <div className="crumbs">
        <Link href="/orgs">Organizations</Link> <span>/</span>{" "}
        <span>{org.slug}</span>
      </div>

      <div className="top">
        <h1>{org.name}</h1>
        <Link className="btn primary" href={`/orgs/${org.slug}/repos/new`}>
          ＋ New repository
        </Link>
      </div>

      {org.description && <p className="subtle">{org.description}</p>}

      <div className="panel">
        <h2>
          Repositories
          <span className="tag">{repos.length}</span>
        </h2>
        {repos.length === 0 ? (
          <div className="empty">
            No repositories yet. Create one — it is initialised in Forgejo with
            a default branch and a README.
          </div>
        ) : (
          repos.map((r) => (
            <Link
              className="row-item"
              key={r.id}
              href={`/orgs/${org.slug}/repos/${r.slug}`}
            >
              <span className="tree-icon">▤</span>
              <span className="nm">{r.name}</span>
              <span className="sub">· {r.slug}</span>
              <span className="spacer" />
              <VisibilityBadge visibility={r.visibility} />
            </Link>
          ))
        )}
      </div>
    </>
  );
}
