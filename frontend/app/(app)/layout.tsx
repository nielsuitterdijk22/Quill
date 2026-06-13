import { Sidebar } from "../components/Sidebar";

// AppLayout is the authenticated shell: a fixed sidebar plus the page body.
// Routes outside this group (e.g. /login) render without the shell.
export default function AppLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  return (
    <div className="app">
      <Sidebar />
      <main className="main">{children}</main>
    </div>
  );
}
