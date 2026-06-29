import { cookies } from "next/headers";
import { redirect } from "next/navigation";

import { Sidebar } from "../components/Sidebar";
import { KeyboardShortcuts } from "../components/KeyboardShortcuts";
import { getToken, requireSession } from "../lib/session";
import { authGet } from "../lib/api/client";
import type { MyProject } from "../lib/api/types";
import { CURRENT_PROJECT_COOKIE } from "../lib/projects";

export default async function AppLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  const user = await requireSession();
  const token = await getToken();

  // Use authGet directly so we can distinguish "no projects" (should redirect)
  // from "fetch failed / auth error" (should not redirect — avoids a loop when
  // Clerk middleware hasn't initialised yet during a soft navigation).
  let projects: MyProject[] = [];
  if (token) {
    const res = await authGet<{ projects?: MyProject[] }>(token, "/api/v1/me/projects");
    if (res.ok) {
      projects = res.data.projects ?? [];
      if (projects.length === 0) {
        redirect("/onboarding");
      }
    }
    // If !res.ok (401, network error, etc.) we fall through with an empty list
    // rather than wrongly redirecting the user back to onboarding.
  }

  const cookieSlug = cookies().get(CURRENT_PROJECT_COOKIE)?.value ?? null;
  const currentProject =
    (cookieSlug && projects.find((p) => p.slug === cookieSlug)?.slug) ||
    projects[0]?.slug ||
    null;

  return (
    <div className="app">
      <Sidebar user={user} projects={projects} currentProject={currentProject} />
      <main className="main">{children}</main>
      <KeyboardShortcuts />
    </div>
  );
}
