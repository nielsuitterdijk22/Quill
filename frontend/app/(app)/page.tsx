import { getMeta, listOrgs, listReposByOrg, type Repo } from "../lib/api";
import { getToken } from "../lib/session";

// Dashboard is the landing page of the authenticated shell. Since PR 4 it shows
// live Forgejo connectivity plus real organization and repository counts sourced
// from the backend; richer browsing arrives in PR 5.
export default async function DashboardPage() {
  const token = getToken();
  const [meta, orgs] = await Promise.all([
    getMeta(),
    token ? listOrgs(token) : Promise.resolve([]),
  ]);
  const online = meta !== null;
  const forgejo = meta?.forgejo;

  // Repo counts per org (and the total) drive the dashboard cards.
  const repoLists = token
    ? await Promise.all(orgs.map((o) => listReposByOrg(token, o.slug)))
    : [];
  const reposByOrg = new Map<string, Repo[]>();
  orgs.forEach((o, i) => reposByOrg.set(o.slug, repoLists[i] ?? []));
  const totalRepos = repoLists.reduce((sum, list) => sum + list.length, 0);

  return (
    <>
      <div className="top">
        <h1>Dashboard</h1>
        <span className="pill">
          backend{" "}
          {online ? (
            <b className="ok">{meta?.version}</b>
          ) : (
            <b className="danger">offline</b>
          )}
        </span>
        <span className="pill">
          forgejo{" "}
          {forgejo?.reachable ? (
            <b className="ok">{forgejo.version ?? "connected"}</b>
          ) : forgejo?.configured ? (
            <b className="danger">unreachable</b>
          ) : (
            <b className="muted">not configured</b>
          )}
        </span>
      </div>

      {!online && (
        <div className="banner">
          Can&apos;t reach the Quill backend. Start it with{" "}
          <span className="mono">make be-run</span> or{" "}
          <span className="mono">make up</span>.
        </div>
      )}

      <div className="cards">
        <div className="card">
          <div className="k">Organizations</div>
          <div className="v">
            {orgs.length} <small>total</small>
          </div>
        </div>
        <div className="card">
          <div className="k">Repositories</div>
          <div className="v">
            {totalRepos} <small>tracked</small>
          </div>
        </div>
        <div className="card">
          <div className="k">Open pull requests</div>
          <div className="v">
            0 <small>across orgs</small>
          </div>
        </div>
        <div className="card">
          <div className="k">Pipelines</div>
          <div className="v">
            0 <small>running</small>
          </div>
        </div>
      </div>

      <div className="panel">
        <h2>
          Organizations
          <span className="tag">PR 4 · forgejo</span>
        </h2>
        {orgs.length === 0 ? (
          <div className="empty">
            No organizations yet. Each org is mirrored into Forgejo and gets a
            default owning team. Repository and org creation is live on the API
            (<span className="mono">POST /api/v1/orgs</span>); browsing UI lands
            in PR 5.
          </div>
        ) : (
          orgs.map((o) => (
            <div className="row-item" key={o.id}>
              <span className="nm">{o.name}</span>
              <span className="sub">
                · {reposByOrg.get(o.slug)?.length ?? 0} repos
              </span>
              <span className="spacer" />
              {o.forgejoOrg ? (
                <span className="tag">forgejo</span>
              ) : (
                <span className="tag">local</span>
              )}
            </div>
          ))
        )}
      </div>

      <div className="panel">
        <h2>Platform services</h2>
        <a className="row-item" href="#">
          <span className="nm">Forge</span>
          <span className="sub">· confidential CI runners</span>
          <span className="spacer" />
          <span className="tag">soon</span>
        </a>
        <a className="row-item" href="#">
          <span className="nm">Yaly</span>
          <span className="sub">· software catalog &amp; self-service</span>
          <span className="spacer" />
          <span className="tag">soon</span>
        </a>
      </div>
    </>
  );
}
