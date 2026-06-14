import { getMeta } from "../lib/api";

// Dashboard is the landing page of the authenticated shell. For PR 1 it confirms
// end-to-end connectivity to the backend and previews the design system; real
// repo/PR/pipeline data lands in later PRs.
export default async function DashboardPage() {
  const meta = await getMeta();
  const online = meta !== null;

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
          <div className="k">Repositories</div>
          <div className="v">
            0 <small>tracked</small>
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
        <div className="card">
          <div className="k">Teams</div>
          <div className="v">
            0 <small>active</small>
          </div>
        </div>
      </div>

      <div className="panel">
        <h2>
          Getting started
          <span className="tag">PR 3 · auth</span>
        </h2>
        <div className="empty">
          You&apos;re signed in. Local username/password auth is live behind a
          pluggable provider (OIDC drops in later). Next up: the Forgejo
          integration layer, then org and repository browsing.
        </div>
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
