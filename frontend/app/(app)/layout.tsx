import { redirect } from "next/navigation";

import { Sidebar } from "../components/Sidebar";
import { KeyboardShortcuts } from "../components/KeyboardShortcuts";
import { getToken, requireSession } from "../lib/session";
import { getMyProjects } from "../lib/api";

// AppLayout is the authenticated shell: a fixed sidebar plus the page body.
// requireSession gates every route in this group, redirecting to /sign-in when
// there is no valid session. Routes outside the group (e.g. /sign-in) render
// without the shell.
export default async function AppLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  const user = await requireSession();
  const token = await getToken();
  const projects = token ? await getMyProjects(token) : [];

  // New users with no projects are sent to onboarding.
  if (projects.length === 0) {
    redirect("/onboarding");
  }

  return (
    <div className="app">
      <Sidebar
        user={user}
        projects={projects}
      />
      <main className="main">{children}</main>
      <KeyboardShortcuts />
    </div>
  );
}
