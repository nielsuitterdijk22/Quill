import Link from "next/link";

import { listProjects } from "../../lib/api";
import { getToken } from "../../lib/session";

// ProjectsPage lists every project the signed-in user can see, linking each to
// its repository list. Project creation is open to any authenticated user.
export default async function ProjectsPage() {
  const token = await getToken();
  const projects = token ? await listProjects(token) : [];

  return (
    <>
      <div className="top">
        <h1>Projects</h1>
        <Link className="btn primary" href="/projects/new">
          ＋ New project
        </Link>
      </div>

      <div className="panel">
        <h2>
          All projects
          <span className="tag">{projects.length}</span>
        </h2>
        {projects.length === 0 ? (
          <div className="empty">
            No projects yet. Create one — it is mirrored into Forgejo.
          </div>
        ) : (
          projects.map((o) => (
            <Link className="row-item" key={o.id} href={`/projects/${o.slug}`}>
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
