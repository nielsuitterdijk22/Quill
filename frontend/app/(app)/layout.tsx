import { cookies } from "next/headers";
import Link from "next/link";
import { redirect } from "next/navigation";

import { Sidebar } from "../components/Sidebar";
import { KeyboardShortcuts } from "../components/KeyboardShortcuts";
import { getToken, getSession } from "../lib/session";
import { authGet } from "../lib/api/client";
import { listOrgs } from "../lib/api/organizations";
import type { Organization } from "../lib/api/organizations";
import type { MyProject } from "../lib/api/types";
import { CURRENT_PROJECT_COOKIE } from "../lib/projects";

export default async function AppLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  const user = await getSession();
  const token = await getToken();

  // Anonymous visitors get a bare shell: PUBLIC repositories are browsable
  // without an account (GH-style), so the (app) group stays reachable signed
  // out — the pages that need auth redirect to /sign-in themselves or the
  // backend answers 401/404 for anything non-public.
  if (!user) {
    return (
      <div className="app app-public">
        <header className="public-top">
          <Link className="project-switcher-btn" href="/">
            <span className="dot" />
            Quill
          </Link>
          <span className="spacer" />
          <Link className="pill" href="/sign-in">
            Sign in
          </Link>
        </header>
        <main className="main main-public">{children}</main>
      </div>
    );
  }

  // Use authGet directly so we can distinguish "no projects" (should redirect)
  // from "fetch failed / auth error" (should not redirect — avoids a loop when
  // the auth middleware hasn't initialised yet during a soft navigation).
  let projects: MyProject[] = [];
  let orgs: Organization[] = [];
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
    orgs = await listOrgs(token);
  }

  const cookieSlug = cookies().get(CURRENT_PROJECT_COOKIE)?.value ?? null;
  const currentProject =
    (cookieSlug && projects.find((p) => p.slug === cookieSlug)?.slug) ||
    projects[0]?.slug ||
    null;

  return (
    <div className="app">
      <Sidebar user={user} projects={projects} orgs={orgs} currentProject={currentProject} />
      <main className="main">{children}</main>
      <KeyboardShortcuts />
    </div>
  );
}
