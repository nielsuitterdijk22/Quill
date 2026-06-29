import { getMeta, getOpenPullRequestCount, getMyProjects, listReposByProject, type Repo } from "../lib/api";
import { getToken } from "../lib/session";

export default async function DashboardPage() {
  const token = await getToken();
  // Use the membership-scoped list — never the admin-wide listProjects, which
  // would leak every tenant's projects to the instance admin (first user).
  const [meta, projects] = await Promise.all([
    getMeta(),
    token ? getMyProjects(token) : Promise.resolve([]),
  ]);
  const online = meta !== null;
  const forgejo = meta?.forgejo;

  // Repo counts per project (and the total) drive the dashboard cards.
  const repoLists = token
    ? await Promise.all(projects.map((o) => listReposByProject(token, o.slug)))
    : [];
  const reposByProject = new Map<string, Repo[]>();
  projects.forEach((o, i) => reposByProject.set(o.slug, repoLists[i] ?? []));
  const totalRepos = repoLists.reduce((sum, list) => sum + list.length, 0);

  // Open PR count across all repos, aggregated server-side (best-effort; 0 on failure).
  const totalOpenPRs = token ? await getOpenPullRequestCount(token) : 0;

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
          <div className="k">Projects</div>
          <div className="v">
            {projects.length} <small>total</small>
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
            {totalOpenPRs} <small>across projects</small>
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
        <h2>Projects</h2>
        {projects.length === 0 ? (
          <div className="empty">
            No projects yet. Create one from{" "}
            <a href="/projects">Projects</a> — each is mirrored into Forgejo,
            then you can add repositories and browse their code.
          </div>
        ) : (
          projects.map((o) => (
            <a className="row-item" key={o.id} href={`/projects/${o.slug}`}>
              <span className="nm">{o.name}</span>
              <span className="sub">
                · {reposByProject.get(o.slug)?.length ?? 0} repos
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
