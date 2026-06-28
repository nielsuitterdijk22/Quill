import { cookies } from "next/headers";
import { redirect } from "next/navigation";

import { Sidebar } from "../components/Sidebar";
import { KeyboardShortcuts } from "../components/KeyboardShortcuts";
import { getToken, requireSession } from "../lib/session";
import { getMyProjects } from "../lib/api";
import { CURRENT_PROJECT_COOKIE } from "../lib/projects";

export default async function AppLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  const user = await requireSession();
  const token = await getToken();
  const projects = token ? await getMyProjects(token) : [];

  if (projects.length === 0) {
    redirect("/onboarding");
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
