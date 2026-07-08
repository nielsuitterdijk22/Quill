import Link from "next/link";
import { notFound } from "next/navigation";

import { getToken } from "../../../../lib/session";
import {
  fetchProjectGovernance,
  ProjectGovernanceSettings,
} from "../../../../components/settings/ProjectGovernanceSettings";

// ProjectSettingsPage manages project-scoped governance. Branch policies set here
// apply to every repository in the project, and a repo may only tighten them.
// The project itself inherits its tenant's policies, shown read-only. The
// governance sections are shared with the personal-project tab under /settings
// via ProjectGovernanceSettings.
export default async function ProjectSettingsPage({
  params,
}: {
  params: { project: string };
}) {
  const token = await getToken();
  if (!token) notFound();

  const governance = await fetchProjectGovernance(token, params.project);
  if (!governance.ok) {
    if (governance.status === 404) notFound();
    return (
      <>
        <div className="crumbs">
          <Link href="/projects">Projects</Link> <span>/</span>{" "}
          <Link href={`/projects/${params.project}`}>{params.project}</Link>{" "}
          <span>/</span> <span>Settings</span>
        </div>
        <h1>Project settings</h1>
        <div className="banner">
          {governance.status === 403
            ? "You do not have access to this project's settings."
            : governance.message}
        </div>
      </>
    );
  }

  const { project } = governance.data;

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

      <ProjectGovernanceSettings data={governance.data} />
    </>
  );
}
