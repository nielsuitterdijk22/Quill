import { getMeta, getPulls, listOrgs, listReposByOrg, type Repo } from "../lib/api";
import { getToken } from "../lib/session";

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

  // Open PR count across all repos (best-effort; 0 on any failure).
  let totalOpenPRs = 0;
  if (token && totalRepos > 0) {
    const prResults = await Promise.all(
      orgs.flatMap((o, i) =>
        (repoLists[i] ?? []).map((r) => getPulls(token, o.slug, r.slug, "open")),
      ),
    );
    totalOpenPRs = prResults.reduce(
      (sum, r) => sum + (r.ok ? r.data.pulls.length : 0),
      0,
    );
  }

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
            {totalOpenPRs} <small>across orgs</small>
          </div>
        </div>
        <div className="card">
          <div className="k">Pipelines</div>
          <div className="v">
            — <small className="muted">coming soon</small>
          </div>
        </div>
      </div>

      <div className="panel">
        <h2>Organizations</h2>
        {orgs.length === 0 ? (
          <div className="empty">
            No organizations yet. Create one from{" "}
            <a href="/orgs">Organizations</a> — each is mirrored into Forgejo and
            gets a default owning team, then you can add repositories and browse
            their code.
          </div>
        ) : (
          orgs.map((o) => (
            <a className="row-item" key={o.id} href={`/orgs/${o.slug}`}>
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
            </a>
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
