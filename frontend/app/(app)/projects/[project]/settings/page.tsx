import Link from "next/link";
import { notFound } from "next/navigation";

import { getProjectPolicies } from "../../../../lib/api";
import { getToken } from "../../../../lib/session";
import { PolicyManager } from "../../../../components/policy/PolicyManager";

// ProjectSettingsPage manages project-scoped governance. Branch policies set here
// apply to every repository in the project, and a repo may only tighten them.
// The project itself inherits its tenant's policies, shown read-only.
export default async function ProjectSettingsPage({
  params,
}: {
  params: { project: string };
}) {
  const token = getToken();
  if (!token) notFound();

  const res = await getProjectPolicies(token, params.project);
  if (!res.ok) {
    if (res.status === 404) notFound();
    return (
      <>
        <div className="crumbs">
          <Link href="/projects">Projects</Link> <span>/</span>{" "}
          <Link href={`/projects/${params.project}`}>{params.project}</Link>{" "}
          <span>/</span> <span>Settings</span>
        </div>
        <h1>Project settings</h1>
        <div className="banner">
          {res.status === 403
            ? "You do not have access to this project's settings."
            : res.message}
        </div>
      </>
    );
  }

  const { project, policies, inherited } = res.data;

  return (
    <>
      <div className="crumbs">
        <Link href="/projects">Projects</Link> <span>/</span>{" "}
        <Link href={`/projects/${project.slug}`}>{project.slug}</Link>{" "}
        <span>/</span> <span>Settings</span>
      </div>

      <div className="top">
        <h1>{project.name} settings</h1>
      </div>

      <section className="settings-section">
        <div className="settings-head">
          <h2 className="settings-title">Branch policies</h2>
          <p className="subtle">
            Rules set here apply to every repository in this project. A
            repository may add stricter rules but cannot weaken these. Lock a
            rule to forbid repositories loosening it.
          </p>
        </div>
        <PolicyManager
          target={{ scope: "project", project: project.slug }}
          policies={policies}
          inherited={inherited}
          canLock
        />
      </section>
    </>
  );
}
