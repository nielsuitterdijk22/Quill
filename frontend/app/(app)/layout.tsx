import { Sidebar } from "../components/Sidebar";
import { requireSession } from "../lib/session";

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

  return (
    <div className="app">
      <Sidebar user={user} />
      <main className="main">{children}</main>
    </div>
  );
}
