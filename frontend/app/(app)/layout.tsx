import { Sidebar } from "../components/Sidebar";
import { KeyboardShortcuts } from "../components/KeyboardShortcuts";
import { getToken, requireSession } from "../lib/session";
import { resolveCurrentProject } from "../lib/projects";

// AppLayout is the authenticated shell: a fixed sidebar plus the page body.
// requireSession gates every route in this group, redirecting to /login when
// there is no valid session. Routes outside the group (e.g. /login) render
// without the shell.
export default async function AppLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  const user = await requireSession();
  const token = getToken();
  const resolved = token ? await resolveCurrentProject(token) : null;

  return (
    <div className="app">
      <Sidebar
        user={user}
        projects={resolved?.projects ?? []}
        currentProject={resolved?.current.slug ?? null}
      />
      <main className="main">{children}</main>
      <KeyboardShortcuts />
    </div>
  );
}
